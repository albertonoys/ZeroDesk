package main

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

// CC1101 sub-GHz transceiver on the Cap CC1101 accessory and the SubG Chat
// frame format, ported from CardputerZero/Cap-CC1101-SubG-Chat (MIT,
// src/radio/driver/cc1101.cpp, src/radio/chat_protocol.cpp). The register
// image is the original's RadioLib 7.7 dump for 868 MHz 2-FSK, 2.4 kbit/s,
// 25.4 kHz deviation, 58 kHz RX bandwidth, sync word 0x12AD, variable length
// with CRC, +10 dBm, so both apps hear each other.

const (
	ccSRES      = 0x30
	ccSFSTXON   = 0x31
	ccSRX       = 0x34
	ccSTX       = 0x35
	ccSIDLE     = 0x36
	ccSPWD      = 0x39
	ccSFRX      = 0x3A
	ccSFTX      = 0x3B
	ccRead      = 0x80
	ccBurst     = 0x40
	ccIOCFG2    = 0x00
	ccIOCFG0    = 0x02
	ccFIFOTHR   = 0x03
	ccPARTNUM   = 0x30
	ccVERSION   = 0x31
	ccMARCSTATE = 0x35
	ccTXBYTES   = 0x3A
	ccRXBYTES   = 0x3B
	ccPATABLE   = 0x3E
	ccFIFO      = 0x3F
	ccFIFOSize  = 64

	ccFreqMHz    = 868.0
	ccBitrate    = 2.4 // kbit/s
	ccDeviation  = 25.4
	ccBandwidth  = 58.0
	ccPowerDBm   = 10
	ccSyncWord   = 0x12AD
	ccMaxPayload = 61
	ccRFSwitch   = 14 // RF_SW0
	ccStateRX    = 0x0D
	ccStateIDLE  = 0x01
	ccStateFSTX  = 0x12
	ccGDO2Ant868 = 0x6F // GDO2 drives RF_SW1 high for 868 MHz
)

// ccRegisters is the original's applyRadioLib868LowConfig(), in write order.
var ccRegisters = [][2]byte{
	{0x00, 0x6F}, {0x01, 0x2E}, {0x02, 0x2E}, {0x03, 0x07}, {0x04, 0x12}, {0x05, 0xAD}, {0x06, 0xFF},
	{0x07, 0x64}, {0x08, 0x05}, {0x09, 0x00}, {0x0A, 0x00}, {0x0B, 0x0F}, {0x0C, 0x00}, {0x0D, 0x21},
	{0x0E, 0x62}, {0x0F, 0x76}, {0x10, 0xF6}, {0x11, 0x83}, {0x12, 0x02}, {0x13, 0x02}, {0x14, 0xF8},
	{0x15, 0x40}, {0x16, 0x07}, {0x17, ccMCSM1(0x30)}, {0x18, 0x14}, {0x19, 0x76}, {0x1A, 0x6C}, {0x1B, 0x03},
	{0x1C, 0x40}, {0x1D, 0x91}, {0x1E, 0x87}, {0x1F, 0x6B}, {0x20, 0xF8}, {0x21, 0x56}, {0x22, 0x10},
	{0x23, 0xA9}, {0x24, 0x0A}, {0x25, 0x20}, {0x26, 0x0D}, {0x27, 0x41}, {0x28, 0x00}, {0x29, 0x59},
	{0x2A, 0x7F}, {0x2B, 0x3F}, {0x2C, 0x88}, {0x2D, 0x31}, {0x2E, 0x0B}, {ccPATABLE, 0xC2},
}

// ccMCSM1 keeps the radio in RX after a packet (RXOFF_MODE = RX) and goes
// to IDLE after TX, preserving the CCA bits.
func ccMCSM1(cur byte) byte { return cur&^0x0F | 0x0C }

func ccKnownVersion(v byte) bool {
	switch v {
	case 0x04, 0x14, 0x20, 0x3F, 0x94, 0xBF, 0xFC:
		return true
	}
	return false
}

func ccRSSI(raw byte) float64 { return float64(int8(raw))/2 - 74 }

// ccFreqWord is FREQ2..0 for a carrier in MHz with the 26 MHz crystal.
func ccFreqWord(mhz float64) uint32 { return uint32(mhz * 65536 / 26) }

func ccAirtime(n int) time.Duration {
	return time.Duration(150+int(float64((n+24)*8)/ccBitrate)) * time.Millisecond
}

var (
	errCCCancelled = errors.New("cancelled")
	errCCAbsent    = errors.New("no CC1101 answered")
)

// ccBus is the SPI link to the CC1101 (spidev with kernel chip select).
type ccBus interface {
	transfer(tx []byte) ([]byte, error)
}

type ccRadio struct {
	spi      ccBus
	rfsw     *gpioLine
	stop     *atomic.Bool
	relaxed  bool
	part     byte
	version  byte
	crcBad   int
	received int
}

type ccPacket struct {
	data []byte
	rssi float64
	lqi  byte
	crc  bool
}

func (r *ccRadio) sleep(d time.Duration) error {
	end := time.Now().Add(d)
	for {
		if r.stop != nil && r.stop.Load() {
			return errCCCancelled
		}
		left := time.Until(end)
		if left <= 0 {
			return nil
		}
		time.Sleep(min(left, 10*time.Millisecond))
	}
}

func (r *ccRadio) strobe(c byte) (byte, error) {
	rx, err := r.spi.transfer([]byte{c})
	if err != nil {
		return 0, err
	}
	return rx[0], nil
}

func ccAddr(reg byte) byte {
	if reg > 0x2E && reg < ccPATABLE {
		return reg | ccBurst // status registers need the burst bit
	}
	return reg
}

func (r *ccRadio) readReg(reg byte) (byte, error) {
	rx, err := r.spi.transfer([]byte{ccAddr(reg) | ccRead, 0})
	if err != nil {
		return 0, err
	}
	return rx[1], nil
}

func (r *ccRadio) writeReg(reg, v byte) error {
	_, err := r.spi.transfer([]byte{ccAddr(reg), v})
	return err
}

func (r *ccRadio) writeBurst(reg byte, data []byte) error {
	_, err := r.spi.transfer(append([]byte{reg | ccBurst}, data...))
	return err
}

func (r *ccRadio) marc() (byte, error) {
	v, err := r.readReg(ccMARCSTATE)
	return v & 0x1F, err
}

// begin resets and probes the chip and loads the chat profile.
func (r *ccRadio) begin() error {
	if _, err := r.strobe(ccSRES); err != nil {
		return err
	}
	if err := r.sleep(150 * time.Millisecond); err != nil {
		return err
	}
	for range 10 {
		var err error
		if r.part, err = r.readReg(ccPARTNUM); err != nil {
			return err
		}
		if r.version, err = r.readReg(ccVERSION); err != nil {
			return err
		}
		if ccKnownVersion(r.version) {
			break
		}
		if err := r.sleep(20 * time.Millisecond); err != nil {
			return err
		}
	}
	if !ccKnownVersion(r.version) {
		if r.version == 0x00 || r.version == 0xFF {
			return errCCAbsent
		}
		return fmt.Errorf("CC1101 probe failed: PARTNUM=0x%02X VERSION=0x%02X", r.part, r.version)
	}
	r.relaxed = r.version == 0x3F || r.version == 0x94 || r.version == 0xBF || r.version == 0xFC
	if err := r.standby(); err != nil {
		return err
	}
	for _, kv := range ccRegisters {
		if err := r.writeReg(kv[0], kv[1]); err != nil {
			return err
		}
	}
	if err := r.flushRX(); err != nil {
		return err
	}
	if _, err := r.strobe(ccSFTX); err != nil {
		return err
	}
	return r.antenna()
}

func (r *ccRadio) antenna() error {
	if r.rfsw != nil {
		if err := r.rfsw.set(true); err != nil {
			return err
		}
	}
	return r.writeReg(ccIOCFG2, ccGDO2Ant868)
}

func (r *ccRadio) standby() error {
	if _, err := r.strobe(ccSIDLE); err != nil {
		return err
	}
	if r.relaxed {
		return r.sleep(2 * time.Millisecond)
	}
	end := time.Now().Add(100 * time.Millisecond)
	for {
		s, err := r.marc()
		if err != nil {
			return err
		}
		if s == ccStateIDLE {
			return nil
		}
		if time.Now().After(end) {
			return errors.New("CC1101 standby timeout")
		}
		time.Sleep(time.Millisecond)
	}
}

func (r *ccRadio) flushRX() error {
	for _, c := range []byte{ccSIDLE, 0, ccSFRX, ccSFRX, 0} {
		if c == 0 {
			time.Sleep(2 * time.Millisecond)
			continue
		}
		if _, err := r.strobe(c); err != nil {
			return err
		}
	}
	return nil
}

func (r *ccRadio) startReceive() error {
	if err := r.standby(); err != nil {
		return err
	}
	if err := r.flushRX(); err != nil {
		return err
	}
	if err := r.writeReg(ccIOCFG0, 0x01); err != nil { // GDO0: RX FIFO threshold or end of packet
		return err
	}
	if err := r.writeReg(ccFIFOTHR, 0x0F); err != nil { // TX 1 / RX 64
		return err
	}
	if err := r.antenna(); err != nil {
		return err
	}
	if _, err := r.strobe(ccSRX); err != nil {
		return err
	}
	time.Sleep(2 * time.Millisecond)
	return nil
}

func (r *ccRadio) idle() error {
	if err := r.standby(); err != nil {
		return err
	}
	if err := r.flushRX(); err != nil {
		return err
	}
	if _, err := r.strobe(ccSFTX); err != nil {
		return err
	}
	return r.antenna()
}

// powerDown puts the transceiver to sleep (SPWD) and drops RF_SW0.
func (r *ccRadio) powerDown() {
	_ = r.standby()
	_, _ = r.strobe(ccSPWD)
	if r.rfsw != nil {
		_ = r.rfsw.set(false)
	}
}

func (r *ccRadio) waitRX(n int, timeout time.Duration) (bool, error) {
	end := time.Now().Add(timeout)
	for {
		v, err := r.readReg(ccRXBYTES)
		if err != nil {
			return false, err
		}
		if v&0x80 != 0 {
			return false, nil
		}
		if int(v&0x7F) >= n {
			return true, nil
		}
		if time.Now().After(end) {
			return false, nil
		}
		if err := r.sleep(5 * time.Millisecond); err != nil {
			return false, err
		}
	}
}

// receive polls the RX FIFO for up to timeout and returns one packet.
func (r *ccRadio) receive(timeout time.Duration) (*ccPacket, error) {
	end := time.Now().Add(timeout)
	for {
		v, err := r.readReg(ccRXBYTES)
		if err != nil {
			return nil, err
		}
		if v&0x80 != 0 { // overflow
			if err := r.startReceive(); err != nil {
				return nil, err
			}
			continue
		}
		if v&0x7F == 0 {
			if time.Now().After(end) {
				return nil, nil
			}
			if err := r.sleep(5 * time.Millisecond); err != nil {
				return nil, err
			}
			continue
		}
		n, err := r.readReg(ccFIFO)
		if err != nil {
			return nil, err
		}
		if n == 0 || n > ccFIFOSize-3 {
			if err := r.startReceive(); err != nil {
				return nil, err
			}
			continue
		}
		ok, err := r.waitRX(int(n)+2, time.Second)
		if err != nil {
			return nil, err
		}
		if !ok {
			if err := r.startReceive(); err != nil {
				return nil, err
			}
			continue
		}
		p := &ccPacket{data: make([]byte, n)}
		for i := range p.data {
			if p.data[i], err = r.readReg(ccFIFO); err != nil {
				return nil, err
			}
		}
		rssi, err := r.readReg(ccFIFO)
		if err != nil {
			return nil, err
		}
		lqi, err := r.readReg(ccFIFO)
		if err != nil {
			return nil, err
		}
		p.rssi, p.lqi, p.crc = ccRSSI(rssi), lqi&0x7F, lqi&0x80 != 0
		r.received++
		if !p.crc {
			r.crcBad++
		}
		if s, err := r.marc(); err == nil && s != ccStateRX {
			if err := r.startReceive(); err != nil {
				return nil, err
			}
		}
		return p, nil
	}
}

// transmit sends one variable-length packet and returns to IDLE.
func (r *ccRadio) transmit(data []byte) error {
	if len(data) == 0 || len(data) > ccMaxPayload {
		return errors.New("payload must be 1 to 61 bytes")
	}
	if err := r.standby(); err != nil {
		return err
	}
	if _, err := r.strobe(ccSFTX); err != nil {
		return err
	}
	if !r.relaxed {
		if _, err := r.strobe(ccSFSTXON); err != nil {
			return err
		}
		start := time.Now()
		for {
			s, err := r.marc()
			if err != nil {
				return err
			}
			if s == ccStateFSTX {
				break
			}
			if time.Since(start) > 1600*time.Microsecond {
				_ = r.standby()
				return errors.New("TX oscillator ready timeout")
			}
		}
	}
	if err := r.writeReg(ccFIFO, byte(len(data))); err != nil {
		return err
	}
	if err := r.writeBurst(ccFIFO, data); err != nil {
		return err
	}
	if err := r.antenna(); err != nil {
		return err
	}
	if _, err := r.strobe(ccSTX); err != nil {
		return err
	}
	// the original waits out the airtime rather than trusting MARCSTATE
	serr := r.sleep(ccAirtime(len(data)))
	_ = r.standby()
	_, _ = r.strobe(ccSFTX)
	_ = r.antenna()
	return serr
}

// ---- chat frames (chat_protocol.cpp) ----

const (
	chatHeaderSize = 5
	chatMaxMessage = 56
	chatMaxName    = 10
)

type chatFrameKind int

const (
	chatLegacy chatFrameKind = iota
	chatData
	chatAck
	chatMalformed
)

type chatFrame struct {
	kind    chatFrameKind
	token   uint32
	payload []byte
	sender  string
}

func chatPrintable(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7E {
			return false
		}
	}
	return true
}

func chatEncodeData(token uint32, payload []byte, sender string) ([]byte, error) {
	if token == 0 || token&^0xFFFFFF != 0 {
		return nil, errors.New("token must be a non-zero 24-bit value")
	}
	if len(payload) == 0 || len(payload) > chatMaxMessage {
		return nil, errors.New("message must be 1 to 56 bytes")
	}
	if sender != "" {
		if len(sender) > chatMaxName || !chatPrintable(sender) {
			return nil, errors.New("sender name must be 1 to 10 printable ASCII characters")
		}
		if len(payload)+len(sender)+1 <= chatMaxMessage {
			p := append([]byte{0x80 | byte(len(sender))}, sender...)
			payload = append(p, payload...)
		}
	}
	f := []byte{0xC3, 0xD1, byte(token >> 16), byte(token >> 8), byte(token)}
	return append(f, payload...), nil
}

func chatEncodeAck(token uint32) []byte {
	return []byte{0xC3, 0xA1, byte(token >> 16), byte(token >> 8), byte(token)}
}

func chatDecode(b []byte) chatFrame {
	data := len(b) >= 2 && b[0] == 0xC3 && b[1] == 0xD1
	ack := len(b) >= 2 && b[0] == 0xC3 && b[1] == 0xA1
	if !data && !ack {
		return chatFrame{kind: chatLegacy, payload: b}
	}
	if len(b) < chatHeaderSize {
		return chatFrame{kind: chatMalformed}
	}
	token := uint32(b[2])<<16 | uint32(b[3])<<8 | uint32(b[4])
	if token == 0 {
		return chatFrame{kind: chatMalformed}
	}
	if ack {
		if len(b) == chatHeaderSize {
			return chatFrame{kind: chatAck, token: token}
		}
		return chatFrame{kind: chatMalformed, token: token}
	}
	if len(b) == chatHeaderSize || len(b) > chatHeaderSize+chatMaxMessage {
		return chatFrame{kind: chatMalformed, token: token}
	}
	p := b[chatHeaderSize:]
	if n := int(p[0] & 0x7F); p[0]&0x80 != 0 && n != 0 && n <= chatMaxName && len(p) > 1+n {
		if name := string(p[1 : 1+n]); chatPrintable(name) {
			return chatFrame{kind: chatData, token: token, payload: p[1+n:], sender: name}
		}
	}
	return chatFrame{kind: chatData, token: token, payload: p}
}

// chatText shows a payload the way the original does: printable ASCII,
// everything else as '.'.
func chatText(p []byte) string {
	b := make([]byte, len(p))
	for i, c := range p {
		if c >= 0x20 && c <= 0x7E {
			b[i] = c
		} else {
			b[i] = '.'
		}
	}
	return string(b)
}

// chatBackoff is the retry delay before attempt+1 (80..200 ms).
func chatBackoff(token uint32, attempt int) time.Duration {
	mixed := token ^ uint32(attempt)*0x45D9F3
	return time.Duration(80+mixed%121) * time.Millisecond
}

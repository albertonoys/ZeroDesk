package main

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// SX1262 driver for the Cap LoRa-1262, a port of the RadioLib calls the
// original app makes (CardputerZero/Cap-LoRa-1262, MIT, which uses RadioLib
// 187ef24 by Jan Gromeš, MIT): SX1262::begin(868.0, 125.0, 12, 5, 0x34, 22,
// 20, 3.0, false), setCurrentLimit(140), setDio2AsRfSwitch(true), then
// startReceive/startTransmit/readData. The command sequence and the
// register workarounds follow RadioLib so the radio ends up configured
// exactly like the LCD app's and the two can talk to each other.

// Radio parameters of the original (src/lora/cap_lora_1262.hpp).
const (
	loraFrequencyMHz  = 868.0
	loraBandwidthKHz  = 125.0
	loraSF            = 12
	loraCR            = 5 // 4/5
	loraSyncWord      = 0x34
	loraPowerDBm      = 22
	loraPreamble      = 20
	loraTCXOVolts     = 3.0
	loraCurrentLimit  = 140.0 // mA
	loraSPISpeed      = 1000000
	loraMaxPayload    = 127
	loraSPIDefault    = "/dev/spidev0.1"
	loraGPIOChip      = "/dev/gpiochip0"
	loraPinIRQ        = 23 // DIO1
	loraPinReset      = 26
	loraPinBusy       = 22
	loraBusyTimeout   = 100 * time.Millisecond
	loraVersionRegLen = 16
)

// commands and registers (SX126x datasheet; RadioLib SX126x_commands.h)
const (
	sxCmdNop               = 0x00
	sxCmdSetSleep          = 0x84
	sxCmdSetStandby        = 0x80
	sxCmdSetTx             = 0x83
	sxCmdSetRx             = 0x82
	sxCmdSetRegulatorMode  = 0x96
	sxCmdCalibrate         = 0x89
	sxCmdCalibrateImage    = 0x98
	sxCmdSetPaConfig       = 0x95
	sxCmdSetRxTxFallback   = 0x93
	sxCmdWriteRegister     = 0x0d
	sxCmdReadRegister      = 0x1d
	sxCmdWriteBuffer       = 0x0e
	sxCmdReadBuffer        = 0x1e
	sxCmdSetDioIrqParams   = 0x08
	sxCmdGetIrqStatus      = 0x12
	sxCmdClearIrqStatus    = 0x02
	sxCmdSetDio2RfSwitch   = 0x9d
	sxCmdSetDio3TCXO       = 0x97
	sxCmdSetRfFrequency    = 0x86
	sxCmdSetPacketType     = 0x8a
	sxCmdSetTxParams       = 0x8e
	sxCmdSetModulation     = 0x8b
	sxCmdSetPacketParams   = 0x8c
	sxCmdSetCadParams      = 0x88
	sxCmdSetBufferBase     = 0x8f
	sxCmdGetStatus         = 0xc0
	sxCmdGetRxBufferStatus = 0x13
	sxCmdGetPacketStatus   = 0x14
	sxCmdGetDeviceErrors   = 0x17
	sxCmdClearDeviceErrors = 0x07

	sxRegVersion     = 0x0320
	sxRegSyncWord    = 0x0740
	sxRegIQConfig    = 0x0736
	sxRegSensitivity = 0x0889
	sxRegTxClamp     = 0x08d8
	sxRegOCP         = 0x08e7

	sxIrqTxDone      = 0x0001
	sxIrqRxDone      = 0x0002
	sxIrqHeaderValid = 0x0010
	sxIrqHeaderErr   = 0x0020
	sxIrqCrcErr      = 0x0040
	sxIrqTimeout     = 0x0200
	sxIrqAll         = 0x43ff

	sxXoscStartErr = 0x0020
)

// ---- pure helpers (unit tested) ----

// loraFreqRaw is the SetRfFrequency word: freq * 2^25 / 32 MHz.
func loraFreqRaw(mhz float64) uint32 {
	return uint32(float32(mhz) * float32(uint32(1)<<25) / 32.0)
}

// loraBWCode maps a LoRa bandwidth in kHz to its register code.
func loraBWCode(khz float64) (byte, bool) {
	switch uint8(khz/2 + 0.01) {
	case 3:
		return 0x00, true // 7.8
	case 5:
		return 0x08, true // 10.4
	case 7:
		return 0x01, true // 15.6
	case 10:
		return 0x09, true // 20.8
	case 15:
		return 0x02, true // 31.25
	case 20:
		return 0x0a, true // 41.7
	case 31:
		return 0x03, true // 62.5
	case 62:
		return 0x04, true // 125
	case 125:
		return 0x05, true // 250
	case 250:
		return 0x06, true // 500
	}
	return 0, false
}

// loraLDRO: low data rate optimisation is on when a symbol lasts 16 ms or
// more (RadioLib's automatic rule; both ends must agree).
func loraLDRO(sf int, khz float64) byte {
	if float64(int(1)<<sf)/khz >= 16 {
		return 1
	}
	return 0
}

// loraSyncWordBytes is RadioLib's setSyncWord(sw, 0x44) register value.
func loraSyncWordBytes(sw byte) [2]byte {
	const control = 0x44
	return [2]byte{sw&0xf0 | control>>4, (sw&0x0f)<<4 | control&0x0f}
}

// loraTCXOCode maps a DIO3 TCXO voltage to its code.
func loraTCXOCode(v float64) (byte, bool) {
	for i, x := range []float64{1.6, 1.7, 1.8, 2.2, 2.4, 2.7, 3.0, 3.3} {
		if math.Abs(v-x) < 0.001 {
			return byte(i), true
		}
	}
	return 0, false
}

// loraImageCal returns the CalibrateImage band for a frequency.
func loraImageCal(mhz float64) [2]byte {
	f := int(mhz)
	switch {
	case f >= 902 && f <= 928:
		return [2]byte{0xe1, 0xe9}
	case f >= 863 && f <= 870:
		return [2]byte{0xd7, 0xdb}
	case f >= 779 && f <= 787:
		return [2]byte{0xc1, 0xc5}
	case f >= 470 && f <= 510:
		return [2]byte{0x75, 0x81}
	case f >= 430 && f <= 440:
		return [2]byte{0x6b, 0x6f}
	}
	return [2]byte{0xd7, 0xdb}
}

// loraTimeOnAir is the LoRa packet airtime (Semtech AN1200.13) with an
// explicit header and CRC on.
func loraTimeOnAir(payload, sf int, khz float64, cr, preamble int) time.Duration {
	tsym := float64(int(1)<<sf) / khz // ms
	de := float64(loraLDRO(sf, khz))
	n := math.Ceil(float64(8*payload-4*sf+28+16) / (4 * (float64(sf) - 2*de)))
	n = math.Max(n*float64(cr), 0) + 8
	ms := (float64(preamble)+4.25)*tsym + n*tsym
	return time.Duration(ms * float64(time.Millisecond))
}

// loraStatusError interprets the status byte the chip returns while a
// command's parameters are clocked in (RadioLib SPIparseStatus).
func loraStatusError(st byte) error {
	switch st & 0x0e {
	case 0x06:
		return errors.New("command timeout")
	case 0x08:
		return errors.New("invalid command")
	case 0x0a:
		return errors.New("command failed")
	}
	if st == 0x00 || st == 0xff {
		return errNoSX1262
	}
	return nil
}

// loraPacketStatus decodes GetPacketStatus: RSSI (dBm) and SNR (dB) of the
// last packet.
func loraPacketStatus(rssiPkt, snrPkt byte) (rssi, snr float64) {
	return -float64(rssiPkt) / 2, float64(int8(snrPkt)) / 4
}

var errNoSX1262 = errors.New("SX1262 not responding")

// ---- the chip ----

type sx1262 struct {
	spi     *loraSPI
	reset   *loraGPIOLines // RST, output
	inputs  *loraGPIOLines // BUSY, DIO1
	version string
}

const (
	sxBusyBit = 1 << 0
	sxDIO1Bit = 1 << 1
)

func (r *sx1262) close() {
	r.spi.close()
	r.reset.close()
	r.inputs.close()
}

func (r *sx1262) busy() bool {
	v, err := r.inputs.get()
	return err == nil && v&sxBusyBit != 0
}

func (r *sx1262) dio1() bool {
	v, err := r.inputs.get()
	return err == nil && v&sxDIO1Bit != 0
}

func (r *sx1262) waitBusy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for r.busy() {
		if time.Now().After(deadline) {
			return errors.New("SX1262 BUSY stuck high")
		}
		time.Sleep(200 * time.Microsecond)
	}
	return nil
}

// xfer waits for BUSY low and clocks one command frame.
func (r *sx1262) xfer(frame []byte) ([]byte, error) {
	if err := r.waitBusy(loraBusyTimeout); err != nil {
		return nil, err
	}
	return r.spi.transfer(frame)
}

// cmd writes a command; the status byte returned with the first parameter
// is checked like RadioLib does.
func (r *sx1262) cmd(op byte, params ...byte) error {
	rx, err := r.xfer(append([]byte{op}, params...))
	if err != nil {
		return err
	}
	if len(rx) > 1 {
		if err := loraStatusError(rx[1]); err != nil {
			return fmt.Errorf("cmd 0x%02x: %w", op, err)
		}
	}
	return nil
}

// read issues a read command: op, NOP (status), n data bytes.
func (r *sx1262) read(op byte, n int, params ...byte) ([]byte, error) {
	frame := append([]byte{op}, params...)
	frame = append(frame, make([]byte, n+1)...)
	rx, err := r.xfer(frame)
	if err != nil {
		return nil, err
	}
	return rx[len(frame)-n:], nil
}

func (r *sx1262) writeReg(addr uint16, data ...byte) error {
	return r.cmd(sxCmdWriteRegister, append([]byte{byte(addr >> 8), byte(addr)}, data...)...)
}

func (r *sx1262) readReg(addr uint16, n int) ([]byte, error) {
	return r.read(sxCmdReadRegister, n, byte(addr>>8), byte(addr))
}

func (r *sx1262) updateReg(addr uint16, f func(byte) byte) error {
	b, err := r.readReg(addr, 1)
	if err != nil {
		return err
	}
	return r.writeReg(addr, f(b[0]))
}

// status runs GetStatus and reports a failed previous command
// (RadioLib SPIcheckStream).
func (r *sx1262) status() error {
	rx, err := r.xfer([]byte{sxCmdGetStatus, sxCmdNop})
	if err != nil {
		return err
	}
	return loraStatusError(rx[1])
}

func (r *sx1262) standby() error { return r.cmd(sxCmdSetStandby, 0x00) } // STDBY_RC

func (r *sx1262) irqStatus() (uint16, error) {
	b, err := r.read(sxCmdGetIrqStatus, 2)
	if err != nil {
		return 0, err
	}
	return uint16(b[0])<<8 | uint16(b[1]), nil
}

func (r *sx1262) clearIrq() error { return r.cmd(sxCmdClearIrqStatus, sxIrqAll>>8, sxIrqAll&0xff) }

func (r *sx1262) deviceErrors() uint16 {
	b, err := r.read(sxCmdGetDeviceErrors, 2)
	if err != nil {
		return 0
	}
	return uint16(b[0])<<8 | uint16(b[1])
}

func (r *sx1262) dioIrq(irq, dio1 uint16) error {
	return r.cmd(sxCmdSetDioIrqParams, byte(irq>>8), byte(irq), byte(dio1>>8), byte(dio1), 0, 0, 0, 0)
}

func (r *sx1262) modulation() error {
	bw, _ := loraBWCode(loraBandwidthKHz)
	return r.cmd(sxCmdSetModulation, loraSF, bw, loraCR-4, loraLDRO(loraSF, loraBandwidthKHz))
}

// packetParams: preamble, explicit header, length, CRC on, standard IQ
// (with RadioLib's inverted-IQ register workaround).
func (r *sx1262) packetParams(length byte) error {
	if err := r.updateReg(sxRegIQConfig, func(b byte) byte { return b | 0x04 }); err != nil {
		return err
	}
	return r.cmd(sxCmdSetPacketParams, loraPreamble>>8, loraPreamble&0xff, 0x00, length, 0x01, 0x00)
}

// hardReset pulses RST and waits until the chip accepts STDBY_RC.
func (r *sx1262) hardReset(settle time.Duration) error {
	if err := r.reset.set(0); err != nil {
		return err
	}
	time.Sleep(2 * time.Millisecond)
	if err := r.reset.set(1); err != nil {
		return err
	}
	deadline := time.Now().Add(settle)
	for {
		err := r.standby()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// probe resets the chip and reads its version string (no RF activity).
func (r *sx1262) probe() error {
	var last error = errNoSX1262
	for range 3 {
		if err := r.hardReset(300 * time.Millisecond); err != nil {
			last = err
			time.Sleep(10 * time.Millisecond)
			continue
		}
		b, err := r.readReg(sxRegVersion, loraVersionRegLen)
		if err != nil {
			last = err
			continue
		}
		v := strings.TrimRight(string(b), "\x00\xff ")
		if strings.HasPrefix(v, "SX126") {
			r.version = v
			return nil
		}
		last = fmt.Errorf("%w (version %q)", errNoSX1262, v)
		time.Sleep(10 * time.Millisecond)
	}
	return last
}

// begin is RadioLib's SX1262::begin with the original's parameters plus
// setCurrentLimit(140) and setDio2AsRfSwitch(true).
func (r *sx1262) begin() error {
	if err := r.probe(); err != nil {
		return err
	}
	step := func(name string, err error) error {
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	}
	// TCXO on DIO3, 5 ms start-up
	_ = r.standby()
	if r.deviceErrors()&sxXoscStartErr != 0 {
		_ = r.cmd(sxCmdClearDeviceErrors, 0, 0)
	}
	tc, _ := loraTCXOCode(loraTCXOVolts)
	const tcxoDelay = uint32(5000 / 15.625) // 5 ms in 15.625 µs steps
	if err := step("tcxo", r.cmd(sxCmdSetDio3TCXO, tc, byte(tcxoDelay>>16), byte(tcxoDelay>>8&0xff), byte(tcxoDelay&0xff))); err != nil {
		return err
	}
	// config(): buffer base, LoRa packet type, fallback STDBY_RC, CAD
	// defaults, IRQs off, calibrate everything
	for _, c := range []struct {
		name string
		op   byte
		p    []byte
	}{
		{"buffer base", sxCmdSetBufferBase, []byte{0, 0}},
		{"packet type", sxCmdSetPacketType, []byte{0x01}},
		{"fallback", sxCmdSetRxTxFallback, []byte{0x20}},
		{"cad", sxCmdSetCadParams, []byte{0x03, 9 + 13, 10, 0x00, 0, 0, 0}},
	} {
		if err := step(c.name, r.cmd(c.op, c.p...)); err != nil {
			return err
		}
	}
	if err := step("irq", r.clearIrq()); err != nil {
		return err
	}
	if err := step("irq", r.dioIrq(0, 0)); err != nil {
		return err
	}
	if _, err := r.xfer([]byte{sxCmdCalibrate, 0x7f}); err != nil {
		return step("calibrate", err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := r.waitBusy(time.Second); err != nil {
		return step("calibrate", err)
	}
	if err := step("calibrate", r.status()); err != nil {
		return err
	}
	if err := step("regulator", r.cmd(sxCmdSetRegulatorMode, 0x01)); err != nil { // DC-DC
		return err
	}
	// modulation, sync word, packet parameters, RF switch, frequency, power
	if err := step("modulation", r.modulation()); err != nil {
		return err
	}
	sw := loraSyncWordBytes(loraSyncWord)
	if err := step("sync word", r.writeReg(sxRegSyncWord, sw[0], sw[1])); err != nil {
		return err
	}
	if err := step("packet", r.packetParams(0xff)); err != nil {
		return err
	}
	if err := step("rf switch", r.cmd(sxCmdSetDio2RfSwitch, 0x01)); err != nil {
		return err
	}
	cal := loraImageCal(loraFrequencyMHz)
	if _, err := r.xfer([]byte{sxCmdCalibrateImage, cal[0], cal[1]}); err != nil {
		return step("image calibration", err)
	}
	if err := r.waitBusy(time.Second); err != nil {
		return step("image calibration", err)
	}
	f := loraFreqRaw(loraFrequencyMHz)
	if err := step("frequency", r.cmd(sxCmdSetRfFrequency, byte(f>>24), byte(f>>16), byte(f>>8), byte(f))); err != nil {
		return err
	}
	if err := step("pa clamp", r.updateReg(sxRegTxClamp, func(b byte) byte { return b | 0x1e })); err != nil {
		return err
	}
	// setOutputPower(22): optimised PA table entry {duty 4, hpMax 7}, 200 µs
	// ramp; RadioLib preserves OCP around SetPaConfig
	ocp, err := r.readReg(sxRegOCP, 1)
	if err != nil {
		return step("power", err)
	}
	if err := step("pa config", r.cmd(sxCmdSetPaConfig, 0x04, 0x07, 0x00, 0x01)); err != nil {
		return err
	}
	if err := step("tx params", r.cmd(sxCmdSetTxParams, loraPowerDBm, 0x04)); err != nil {
		return err
	}
	if err := step("ocp", r.writeReg(sxRegOCP, ocp[0])); err != nil {
		return err
	}
	if err := step("current limit", r.writeReg(sxRegOCP, byte(loraCurrentLimit/2.5))); err != nil {
		return err
	}
	return step("rf switch", r.cmd(sxCmdSetDio2RfSwitch, 0x01))
}

// startReceive enters continuous RX (RadioLib startReceive()).
func (r *sx1262) startReceive() error {
	if err := r.standby(); err != nil {
		return err
	}
	if err := r.dioIrq(sxIrqRxDone|sxIrqTimeout|sxIrqCrcErr|sxIrqHeaderValid|sxIrqHeaderErr, sxIrqRxDone); err != nil {
		return err
	}
	if err := r.cmd(sxCmdSetBufferBase, 0, 0); err != nil {
		return err
	}
	if err := r.clearIrq(); err != nil {
		return err
	}
	if err := r.packetParams(0xff); err != nil {
		return err
	}
	return r.cmd(sxCmdSetRx, 0xff, 0xff, 0xff)
}

// startTransmit loads the payload and keys the transmitter; completion is
// signalled by TX_DONE on DIO1.
func (r *sx1262) startTransmit(payload []byte) error {
	if len(payload) == 0 || len(payload) > loraMaxPayload {
		return errors.New("payload length")
	}
	if err := r.standby(); err != nil {
		return err
	}
	if err := r.packetParams(byte(len(payload))); err != nil {
		return err
	}
	if err := r.dioIrq(sxIrqTxDone|sxIrqTimeout, sxIrqTxDone); err != nil {
		return err
	}
	if err := r.cmd(sxCmdSetBufferBase, 0, 0); err != nil {
		return err
	}
	if err := r.cmd(sxCmdWriteBuffer, append([]byte{0}, payload...)...); err != nil {
		return err
	}
	if err := r.clearIrq(); err != nil {
		return err
	}
	if err := r.updateReg(sxRegSensitivity, func(b byte) byte { return b | 0x04 }); err != nil { // not 500 kHz
		return err
	}
	if err := r.cmd(sxCmdSetTx, 0, 0, 0); err != nil {
		return err
	}
	return r.waitBusy(loraBusyTimeout) // PA ramp-up done
}

// finishTransmit clears the IRQs and returns to standby.
func (r *sx1262) finishTransmit() error {
	if err := r.clearIrq(); err != nil {
		return err
	}
	return r.standby()
}

type loraPacket struct {
	data     []byte
	rssi     float64
	snr      float64
	crcError bool
}

// readPacket fetches a received packet (RadioLib readData + getRSSI/getSNR).
func (r *sx1262) readPacket(irq uint16) (loraPacket, error) {
	var p loraPacket
	p.crcError = irq&sxIrqCrcErr != 0 || (irq&sxIrqHeaderErr != 0 && irq&sxIrqHeaderValid == 0)
	st, err := r.read(sxCmdGetRxBufferStatus, 2)
	if err != nil {
		return p, err
	}
	n, off := int(st[0]), st[1]
	if n > 0 {
		b, err := r.read(sxCmdReadBuffer, n, off)
		if err != nil {
			return p, err
		}
		p.data = b
	}
	if err := r.clearIrq(); err != nil {
		return p, err
	}
	ps, err := r.read(sxCmdGetPacketStatus, 3)
	if err == nil {
		p.rssi, p.snr = loraPacketStatus(ps[0], ps[1])
	}
	return p, nil
}

// sleep puts the chip in warm-start sleep (RadioLib sleep(true)).
func (r *sx1262) sleep() {
	_ = r.standby()
	if r.waitBusy(loraBusyTimeout) == nil {
		_, _ = r.spi.transfer([]byte{sxCmdSetSleep, 0x04})
	}
	time.Sleep(time.Millisecond)
}

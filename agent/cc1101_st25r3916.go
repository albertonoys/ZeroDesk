package main

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

// ST25R3916 NFC reader on the Cap CC1101 accessory: NFC-A discovery
// (WUPA, anticollision, select, HLTA), NFC-F SENSF_REQ, and Type 2 reads.
// Ported from CardputerZero/Cap-CC1101-NFC (MIT, src/nfc/st25r3916_driver.cpp),
// which derives its register sequences from M5Stack M5Unit-NFC (MIT,
// Copyright (c) 2025 M5Stack Technology CO LTD). The C++ exceptions become a
// sticky error: every register operation is a no-op once d.err is set, and
// the public entry points return it.
//
// The driver only ever sends reader commands (WUPA, anticollision, SELECT,
// HLTA, READ, GET_VERSION, SENSF_REQ). It has no tag write path.

const (
	stRegIOConf1        = 0x00
	stRegOpControl      = 0x02
	stRegMode           = 0x03
	stRegBitrate        = 0x04
	stRegISO14443A      = 0x05
	stRegFelica         = 0x07
	stRegPassiveTarget  = 0x08
	stRegAux            = 0x0A
	stRegRxConf1        = 0x0B
	stRegNoResponse1    = 0x10
	stRegTimerEMV       = 0x12
	stRegFieldOnGuard   = 0x15
	stRegMaskMainIRQ    = 0x16
	stRegMainIRQ        = 0x1A
	stRegErrorIRQ       = 0x1C
	stRegPassiveIRQ     = 0x1D
	stRegFIFOStatus1    = 0x1E
	stRegCollision      = 0x20
	stRegTxBytes1       = 0x22
	stRegADC            = 0x25
	stRegAntTune1       = 0x26
	stRegAntTune2       = 0x27
	stRegTxDriver       = 0x28
	stRegPTModulation   = 0x29
	stRegExtFieldOn     = 0x2A
	stRegExtFieldOff    = 0x2B
	stRegAuxDisplay     = 0x31
	stRegICIdentity     = 0x3F
	stRegBEMD           = 0x05
	stRegBCorr1         = 0x0C
	stRegBCorr2         = 0x0D
	stRegBResAM         = 0x2A
	stRegBRegulator     = 0x2C
	stRegBOvershoot1    = 0x30
	stRegBOvershoot2    = 0x31
	stRegBUndershoot1   = 0x32
	stRegBUndershoot2   = 0x33
	stCmdSetDefault     = 0xC1
	stCmdStopAll        = 0xC2
	stCmdTxWithCRC      = 0xC4
	stCmdTxWithoutCRC   = 0xC5
	stCmdTxWUPA         = 0xC7
	stCmdInitialFieldOn = 0xC8
	stCmdResetRxGain    = 0xD5
	stCmdAdjustRegs     = 0xD6
	stCmdClearFIFO      = 0xDB
	stCmdMeasureVDD     = 0xDF
	stCmdSpaceB         = 0xFB
	stCmdTestAccess     = 0xFC

	stOpOscillator = 0x80
	stOpReceiver   = 0x40
	stOpTx         = 0x08
	stOpWakeup     = 0x04
	stOpFieldDet   = 0x03
	stOpFieldCA    = 0x01

	stAuxNoCRCRx    = 0x80
	stAuxNoCorr     = 0x04
	stAuxDispTxOn   = 0x20
	stAuxDispOscOK  = 0x10
	stTimerNRTEMV   = 0x02
	stReadReg       = 0x40
	stLoadFIFO      = 0x80
	stReadFIFO      = 0x9F
	stIdentityMask  = 0xF8
	stIdentity3916  = 0x28
	stIdentity3916B = 0x30
	stMaxFIFO       = 512

	stIRQOsc      = 0x80000000
	stIRQRxStart  = 0x20000000
	stIRQRxEnd    = 0x10000000
	stIRQTxEnd    = 0x08000000
	stIRQColl     = 0x04000000
	stIRQNoResp   = 0x00400000
	stIRQFieldCol = 0x00040000
	stIRQGuard    = 0x00020000
	stIRQCRC      = 0x00008000
	stIRQParity   = 0x00004000
	stIRQHard     = 0x00003000
	stIRQRxErrors = stIRQCRC | stIRQParity | stIRQHard

	stNfcFPollTimeout = 40 * time.Millisecond
	stNfcFMinResponse = 18
	stNfcFMaxResponse = 20
	stType2MaxPages   = 256
)

var stSensfReq = []byte{0x00, 0xFF, 0xFF, 0x00, 0x03}

var (
	errSTCancelled = errors.New("cancelled")
	// errSTContended: LCD traffic hit the bus while the reader's software
	// chip select was asserted, so the chip may have seen foreign bytes.
	errSTContended = errors.New("SPI bus contended")
)

// stTransport is one ST25R3916 attachment: xfer clocks a whole frame with
// the chip selected; irq reads the IRQ line; waitIRQ waits for its rising edge.
type stTransport interface {
	xfer(tx []byte) ([]byte, error)
	irq() bool
	waitIRQ(d time.Duration) bool
}

type stChipInfo struct {
	Identity  uint8 `json:"identity"`
	Revision  uint8 `json:"revision"`
	Type      uint8 `json:"type"`
	VDD       uint8 `json:"vdd"`
	Regulator uint8 `json:"regulator"`
}

type stPollKind int

const (
	stNoTag stPollKind = iota
	stInconclusive
	stTag
)

type stNfcATag struct {
	ATQA      [2]byte
	UID       []byte
	SAK       byte
	TypeName  string
	Version   []byte // GET_VERSION response, Type 2 only
	Pages     int    // total pages from GET_VERSION / CC, 0 if unknown
	Memory    []byte // Type 2 pages read from page 0
	NDEF      bool
	NDEFRead  bool
	NDEFCap   int
	NDEFBytes []byte
}

type stNfcFTag struct {
	IDm, PMm [8]byte
}

type stDriver struct {
	t      stTransport
	stop   *atomic.Bool
	err    error
	stored uint32

	ready, discovery  bool
	nfcF              bool // active protocol
	fieldReset        bool
	failedWakeups     int
	noTags, noTagsF   int
	cached            *stNfcATag
	misoPullDeselect  bool
	misoPullSelected  bool
	contentionRetries int
}

func newSTDriver(t stTransport, stop *atomic.Bool) *stDriver {
	return &stDriver{t: t, stop: stop, misoPullDeselect: true, misoPullSelected: true}
}

// ---- register access ----

func (d *stDriver) fail(err error) {
	if d.err == nil && err != nil {
		d.err = err
	}
}

func (d *stDriver) exchange(tx []byte) []byte {
	if d.err != nil {
		return make([]byte, len(tx))
	}
	rx, err := d.t.xfer(tx)
	if err != nil {
		d.fail(err)
		return make([]byte, len(tx))
	}
	return rx
}

func (d *stDriver) readRegs(reg byte, n int) []byte {
	tx := make([]byte, n+1)
	tx[0] = stReadReg | reg&0x3F
	return d.exchange(tx)[1:]
}

func (d *stDriver) readReg(reg byte) byte { return d.readRegs(reg, 1)[0] }

func (d *stDriver) readReg16(reg byte) uint16 {
	b := d.readRegs(reg, 2)
	return uint16(b[0])<<8 | uint16(b[1])
}

func (d *stDriver) writeRegs(reg byte, v ...byte) {
	d.exchange(append([]byte{reg & 0x3F}, v...))
}

func (d *stDriver) writeReg16(reg byte, v uint16) { d.writeRegs(reg, byte(v>>8), byte(v)) }

func (d *stDriver) writeReg32(reg byte, v uint32) {
	d.writeRegs(reg, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func (d *stDriver) readRegB(reg byte) byte {
	return d.exchange([]byte{stCmdSpaceB, stReadReg | reg&0x3F, 0})[2]
}

func (d *stDriver) writeRegB(reg, v byte) { d.exchange([]byte{stCmdSpaceB, reg & 0x3F, v}) }

func (d *stDriver) cmd(c byte, data ...byte) { d.exchange(append([]byte{c}, data...)) }

func (d *stDriver) modify(reg, set, clear byte) {
	cur := d.readReg(reg)
	upd := cur&^clear | set
	if upd != cur {
		d.writeRegs(reg, upd)
	}
}

func (d *stDriver) sleep(dur time.Duration) {
	end := time.Now().Add(dur)
	for d.err == nil {
		if d.stop != nil && d.stop.Load() {
			d.fail(errSTCancelled)
			return
		}
		left := time.Until(end)
		if left <= 0 {
			return
		}
		time.Sleep(min(left, 10*time.Millisecond))
	}
}

func (d *stDriver) cancelled() bool {
	if d.stop != nil && d.stop.Load() {
		d.fail(errSTCancelled)
	}
	return d.err != nil
}

func (d *stDriver) readIRQs() uint32 {
	errIRQ := d.readReg(stRegErrorIRQ)
	main := d.readReg16(stRegMainIRQ)
	passive := d.readReg(stRegPassiveIRQ)
	return uint32(main)<<16 | uint32(errIRQ)<<8 | uint32(passive)
}

func (d *stDriver) clearIRQs() {
	d.stored = 0
	for range 4 {
		d.readIRQs()
		if d.err != nil || !d.t.irq() {
			return
		}
	}
}

func (d *stDriver) waitIRQ(flags uint32, timeout time.Duration) uint32 {
	deadline := time.Now().Add(max(timeout, 0))
	for !d.cancelled() {
		if d.t.irq() {
			for range 4 {
				d.stored |= d.readIRQs()
				if !d.t.irq() {
					break
				}
			}
		}
		if m := d.stored & flags; m != 0 {
			d.stored &^= m
			return m
		}
		left := time.Until(deadline)
		if left <= 0 {
			return 0
		}
		d.t.waitIRQ(max(min(left, 2*time.Millisecond), time.Millisecond))
		// polling is the fallback for a missed edge
		d.stored |= d.readIRQs()
	}
	return 0
}

func (d *stDriver) fifoSize() int {
	s := d.readReg16(stRegFIFOStatus1)
	return int(s>>8) | int(s&0xC0)<<2
}

func (d *stDriver) readFIFO(capacity int) []byte {
	n := min(d.fifoSize(), capacity)
	if n <= 0 || d.err != nil {
		return nil
	}
	tx := make([]byte, n+1)
	tx[0] = stReadFIFO
	return d.exchange(tx)[1:]
}

func (d *stDriver) writeFIFO(b []byte) {
	if len(b) == 0 || len(b) > stMaxFIFO {
		d.fail(errors.New("invalid FIFO write size"))
		return
	}
	d.exchange(append([]byte{stLoadFIFO}, b...))
}

func (d *stDriver) setTxLength(bytes int, bits byte) {
	d.writeReg16(stRegTxBytes1, uint16(bytes)<<3|uint16(bits&7))
}

func stNoResponseTimer(timeout time.Duration, slow bool) uint16 {
	const carrier = 13_560_000
	step := uint64(64)
	if slow {
		step = 4096
	}
	ms := uint64(max(1, timeout.Milliseconds()))
	den := step * 1000
	v := (ms*carrier + den - 1) / den
	return uint16(max(1, min(v, 0xFFFF)))
}

func (d *stDriver) setNoResponse(timeout time.Duration) {
	slow := d.readReg(stRegTimerEMV)&0x01 != 0
	d.writeReg16(stRegNoResponse1, stNoResponseTimer(timeout, slow))
}

func (d *stDriver) prepareTransceive() {
	d.cmd(stCmdStopAll)
	d.cmd(stCmdResetRxGain)
}

// ---- setup ----

func (d *stDriver) initialize() (stChipInfo, error) {
	d.err = nil
	d.shutdown()
	d.err = nil
	d.sleep(50 * time.Millisecond)
	var chip stChipInfo
	for attempt := 0; attempt < 5 && d.err == nil; attempt++ {
		chip.Identity = d.readReg(stRegICIdentity)
		chip.Type, chip.Revision = chip.Identity>>3, chip.Identity&7
		if chip.Identity&stIdentityMask == stIdentity3916 {
			break
		}
		d.sleep(20 * time.Millisecond)
	}
	if d.err != nil {
		return chip, d.err
	}
	if chip.Identity&stIdentityMask != stIdentity3916 {
		return chip, &stIdentityError{chip.Identity}
	}
	d.cmd(stCmdStopAll)
	d.modify(stRegOpControl, 0, stOpTx|stOpReceiver)
	d.sleep(2 * time.Millisecond)
	// ST errata: this test-access frame must follow Set Default
	d.cmd(stCmdSetDefault)
	d.cmd(stCmdTestAccess, 0x04, 0x10)
	pull := byte(0)
	if d.misoPullDeselect {
		pull |= 0x08
	}
	if d.misoPullSelected {
		pull |= 0x10
	}
	d.writeRegs(stRegIOConf1, 0x07, 0x24|pull)
	d.writeRegs(stRegTxDriver, 0xD0)
	d.writeRegB(stRegBResAM, 0x80)
	d.writeRegB(stRegBResAM, 0x00)
	d.writeRegs(stRegExtFieldOn, 0x13)
	d.writeRegs(stRegExtFieldOff, 0x02)
	d.modify(stRegPassiveTarget, 0x50, 0xF0)
	d.writeRegs(stRegPTModulation, 0x5F)
	d.writeRegB(stRegBEMD, 0x40)
	d.writeRegs(stRegAntTune1, 0x82)
	d.writeRegs(stRegAntTune2, 0x82)
	d.cmd(stCmdClearFIFO)
	d.writeReg32(stRegMaskMainIRQ, 0xFFFFFFFF)
	d.clearIRQs()
	d.enableOscillator()
	d.writeReg32(stRegMaskMainIRQ, 0)
	d.cmd(stCmdMeasureVDD)
	d.sleep(time.Millisecond)
	chip.VDD = d.readReg(stRegADC)
	d.cmd(stCmdAdjustRegs)
	d.sleep(5 * time.Millisecond)
	chip.Regulator = d.readRegB(stRegBRegulator)
	d.configureNfcA()
	if d.err != nil {
		return chip, d.err
	}
	d.ready, d.discovery, d.nfcF, d.fieldReset = true, true, false, false
	d.failedWakeups, d.noTags, d.noTagsF = 0, 0, 0
	return chip, nil
}

type stIdentityError struct{ id uint8 }

func (e *stIdentityError) Error() string {
	return fmt.Sprintf("unexpected IC identity 0x%02X", e.id)
}

func (d *stDriver) enableOscillator() {
	d.modify(stRegMaskMainIRQ, 0, 0x80)
	d.clearIRQs()
	d.modify(stRegOpControl, stOpOscillator, 0)
	d.waitIRQ(stIRQOsc, 50*time.Millisecond)
	for range 10 {
		if d.cancelled() {
			return
		}
		if d.readReg(stRegAuxDisplay)&stAuxDispOscOK != 0 {
			return
		}
		d.sleep(time.Millisecond)
	}
	d.fail(errors.New("oscillator did not become stable"))
}

func (d *stDriver) enableField() {
	d.cmd(stCmdStopAll)
	d.modify(stRegOpControl, 0, stOpTx|stOpReceiver|stOpWakeup|stOpFieldDet)
	d.modify(stRegOpControl, stOpFieldCA, stOpFieldDet)
	d.writeRegB(stRegFieldOnGuard, 33) // 75 us + 33 * 151 us = 5.06 ms
	d.clearIRQs()
	d.cmd(stCmdInitialFieldOn)
	flags := d.waitIRQ(stIRQGuard|stIRQFieldCol, 15*time.Millisecond)
	if d.err != nil {
		return
	}
	if flags&stIRQFieldCol != 0 {
		d.fail(errors.New("external RF field detected"))
		return
	}
	if flags&stIRQGuard == 0 {
		d.fail(errors.New("field-on guard timer did not complete"))
		return
	}
	d.modify(stRegOpControl, stOpReceiver, stOpWakeup)
	if op := d.readReg(stRegOpControl); d.err == nil && op&stOpTx == 0 {
		d.fail(errors.New("RF transmitter enable was not retained"))
	}
}

func (d *stDriver) configureNfcA() {
	if d.cancelled() {
		return
	}
	d.cmd(stCmdStopAll)
	d.modify(stRegOpControl, 0, stOpWakeup)
	d.writeRegs(stRegMode, 0x08)
	d.writeRegs(stRegBitrate, 0x00)
	d.writeRegs(stRegISO14443A, 0x00)
	d.modify(stRegTimerEMV, 0, stTimerNRTEMV)
	d.modify(stRegAux, 0, stAuxNoCorr)
	d.writeRegB(stRegBOvershoot1, 0x40)
	d.writeRegB(stRegBOvershoot2, 0x03)
	d.writeRegB(stRegBUndershoot1, 0x40)
	d.writeRegB(stRegBUndershoot2, 0x03)
	d.writeRegB(stRegBCorr1, 0x47)
	d.writeRegB(stRegBCorr2, 0x00)
	d.writeRegs(stRegRxConf1, 0x08, 0x2D, 0xD8, 0x22)
	d.cmd(stCmdResetRxGain)
	d.writeReg32(stRegMaskMainIRQ, 0)
	d.clearIRQs()
	d.enableField()
}

func (d *stDriver) configureNfcF() {
	if d.cancelled() {
		return
	}
	d.cmd(stCmdStopAll)
	d.modify(stRegOpControl, 0, stOpWakeup|stOpFieldDet)
	// M5Unit-NFC configure_nfc_f(): FeliCa initiator, 212 kbit/s both ways
	d.writeRegs(stRegMode, 0x1C)
	d.writeRegs(stRegBitrate, 0x11)
	d.writeRegs(stRegFelica, 0x00)
	d.writeRegs(stRegAux, 0x00)
	d.modify(stRegTimerEMV, stTimerNRTEMV, 0)
	d.writeRegB(stRegBCorr1, 0x54)
	d.writeRegB(stRegBCorr2, 0x00)
	d.writeRegs(stRegRxConf1, 0x13, 0x3D, 0x00, 0x00)
	d.cmd(stCmdResetRxGain)
	d.writeReg32(stRegMaskMainIRQ, 0)
	d.clearIRQs()
	d.enableField()
}

func (d *stDriver) resetField(nfcF bool) {
	d.cmd(stCmdStopAll)
	d.writeRegs(stRegOpControl, stOpOscillator)
	d.sleep(6 * time.Millisecond)
	if nfcF {
		d.configureNfcF()
	} else {
		d.configureNfcA()
	}
}

// ensureField re-checks the operating state and profile registers before
// each exchange and reconfigures when they drifted (a lost write on the
// shared, level-translated bus, or foreign bytes on a software chip select).
func (d *stDriver) ensureField(nfcF bool) {
	const mask = stOpOscillator | stOpReceiver | stOpTx | stOpWakeup | stOpFieldDet
	const want = stOpOscillator | stOpReceiver | stOpTx | stOpFieldCA
	profile := func() bool {
		mb := d.readRegs(stRegMode, 2)
		rx := d.readRegs(stRegRxConf1, 4)
		timer := d.readReg(stRegTimerEMV)
		if nfcF {
			return mb[0] == 0x1C && mb[1] == 0x11 && rx[0] == 0x13 && rx[1] == 0x3D && rx[2] == 0 && rx[3] == 0 && timer&stTimerNRTEMV != 0
		}
		return mb[0] == 0x08 && mb[1] == 0x00 && rx[0] == 0x08 && rx[1] == 0x2D && rx[2] == 0xD8 && rx[3] == 0x22 && timer&stTimerNRTEMV == 0
	}
	var op byte
	for i := range 3 {
		if i > 0 {
			d.sleep(time.Millisecond)
		}
		op = d.readReg(stRegOpControl)
		if d.err != nil {
			return
		}
		if op&mask == want && profile() {
			return
		}
	}
	if op&stOpOscillator == 0 {
		d.enableOscillator()
	}
	if nfcF {
		d.configureNfcF()
	} else {
		d.configureNfcA()
	}
}

// shutdown switches the RF field off and masks interrupts; it ignores
// errors (it also runs on the error path).
func (d *stDriver) shutdown() {
	saved := d.err
	d.err = nil
	d.cmd(stCmdStopAll)
	d.modify(stRegOpControl, 0, stOpTx|stOpReceiver)
	d.writeReg32(stRegMaskMainIRQ, 0xFFFFFFFF)
	d.err = saved
	d.ready, d.discovery, d.nfcF, d.fieldReset = false, false, false, false
	d.stored, d.failedWakeups, d.noTags, d.noTagsF = 0, 0, 0, 0
	d.cached = nil
}

// fieldOff stops discovery but keeps the chip configured.
func (d *stDriver) fieldOff() {
	saved := d.err
	d.err = nil
	d.cmd(stCmdStopAll)
	d.modify(stRegOpControl, 0, stOpTx|stOpReceiver)
	d.err = saved
	d.discovery, d.fieldReset, d.cached = false, false, nil
}

func (d *stDriver) startDiscovery(nfcF bool) error {
	d.err = nil
	if !d.ready {
		return errors.New("reader not initialized")
	}
	if d.discovery && d.nfcF == nfcF {
		return nil
	}
	if nfcF {
		d.configureNfcF()
	} else {
		d.configureNfcA()
	}
	d.discovery, d.nfcF, d.fieldReset = true, nfcF, false
	d.failedWakeups, d.noTags, d.noTagsF = 0, 0, 0
	return d.err
}

// ---- NFC-A ----

func stSelectCmd(level int) byte { return byte(0x91 + level*2) }

func stTypeForSAK(sak byte) string {
	switch sak {
	case 0x00:
		return "NFC Forum Type 2"
	case 0x08, 0x28:
		return "MIFARE Classic 1K"
	case 0x09:
		return "MIFARE Classic Mini"
	case 0x18, 0x38:
		return "MIFARE Classic 4K"
	}
	if sak&0x20 != 0 {
		return "ISO-DEP (Type 4)"
	}
	return "NFC-A"
}

type stWakeup int

const (
	stWakeNoTag stWakeup = iota
	stWakeInconclusive
	stWakeResponse
)

func (d *stDriver) requestWakeup() (stWakeup, [2]byte) {
	var atqa [2]byte
	d.ensureField(false)
	d.prepareTransceive()
	d.setNoResponse(4 * time.Millisecond)
	d.writeRegs(stRegISO14443A, 0x00)
	d.modify(stRegAux, stAuxNoCRCRx, 0)
	d.clearIRQs()
	d.cmd(stCmdClearFIFO)
	d.setTxLength(0, 0)
	d.cmd(stCmdTxWUPA)
	flags := d.waitIRQ(stIRQRxEnd|stIRQColl|stIRQNoResp|stIRQRxErrors, 6*time.Millisecond)
	if d.err != nil {
		return stWakeInconclusive, atqa
	}
	activity := (flags|d.stored)&(stIRQRxStart|stIRQRxEnd|stIRQColl|stIRQRxErrors) != 0
	if flags&stIRQRxErrors != 0 || flags&(stIRQRxEnd|stIRQColl) == 0 {
		d.failedWakeups++
		if flags&stIRQNoResp != 0 && !activity {
			return stWakeNoTag, atqa
		}
		return stWakeInconclusive, atqa
	}
	for i := 0; i < 3 && d.fifoSize() < 2; i++ {
		d.sleep(time.Millisecond)
	}
	b := d.readFIFO(2)
	if len(b) != 2 {
		return stWakeInconclusive, atqa
	}
	copy(atqa[:], b)
	return stWakeResponse, atqa
}

func (d *stDriver) anticollision(level int) ([5]byte, bool) {
	var resp [5]byte
	d.setNoResponse(8 * time.Millisecond)
	d.writeRegs(stRegISO14443A, 0x01)
	d.modify(stRegAux, 0, stAuxNoCRCRx)
	frame := [7]byte{stSelectCmd(level), 0x20}
	sentBytes, sentBits, offset := 2, byte(0), 0
	for range 32 {
		d.prepareTransceive()
		d.clearIRQs()
		d.cmd(stCmdClearFIFO)
		n := sentBytes
		if sentBits != 0 {
			n++
		}
		d.writeFIFO(frame[:n])
		d.setTxLength(sentBytes, sentBits)
		d.cmd(stCmdTxWithoutCRC)
		flags := d.waitIRQ(stIRQRxEnd|stIRQColl|stIRQNoResp|stIRQRxErrors, 10*time.Millisecond)
		coll := flags&stIRQColl != 0
		if d.err != nil || (!coll && flags&stIRQRxEnd == 0) {
			return resp, false
		}
		if offset >= len(resp) {
			return resp, false
		}
		got := d.readFIFO(len(resp) - offset)
		if len(got) == 0 {
			return resp, false
		}
		copy(resp[offset:], got)
		if !coll {
			return resp, true
		}
		cd := d.readReg(stRegCollision)
		collBytes, collBits := int(cd>>4&0x0F), cd>>1&0x07
		if offset+len(got) > len(resp) || collBytes < 2 || collBytes > 6 {
			return resp, false
		}
		cb := resp[offset+len(got)-1] | 1<<collBits
		sentBytes = collBytes
		if collBits == 7 {
			sentBytes++
		}
		sentBits = (collBits + 1) & 7
		if sentBytes >= len(frame) {
			return resp, false
		}
		frame[1] = byte(sentBytes<<4) | sentBits
		copy(frame[2+offset:], resp[offset:offset+len(got)])
		frame[sentBytes] = cb
		offset = len(got) - 1
		if sentBits != 0 {
			resp[offset] = resp[offset]>>sentBits<<sentBits | cb
		}
	}
	return resp, false
}

func (d *stDriver) transceive(tx []byte, capacity, minimum int, timeout time.Duration) []byte {
	d.prepareTransceive()
	d.setNoResponse(timeout)
	d.writeRegs(stRegISO14443A, 0x00)
	d.modify(stRegAux, 0, stAuxNoCRCRx)
	d.clearIRQs()
	d.cmd(stCmdClearFIFO)
	d.writeFIFO(tx)
	d.setTxLength(len(tx), 0)
	d.cmd(stCmdTxWithCRC)
	flags := d.waitIRQ(stIRQRxEnd|stIRQNoResp|stIRQRxErrors, timeout+2*time.Millisecond)
	if d.err != nil || flags&(stIRQNoResp|stIRQRxErrors) != 0 || flags&stIRQRxEnd == 0 {
		return nil
	}
	for i := 0; i < 3 && d.fifoSize() < minimum; i++ {
		d.sleep(time.Millisecond)
	}
	b := d.readFIFO(capacity)
	if len(b) < minimum {
		return nil
	}
	return b
}

func (d *stDriver) selectLevel(level int, ac [5]byte) (byte, bool) {
	frame := append([]byte{stSelectCmd(level), 0x70}, ac[:]...)
	r := d.transceive(frame, 3, 3, 8*time.Millisecond)
	if len(r) != 3 {
		return 0, false
	}
	return r[0], true
}

func (d *stDriver) halt() bool {
	d.prepareTransceive()
	d.writeRegs(stRegISO14443A, 0x00)
	d.modify(stRegAux, 0, stAuxNoCRCRx)
	d.clearIRQs()
	d.cmd(stCmdClearFIFO)
	d.writeFIFO([]byte{0x50, 0x00})
	d.setTxLength(2, 0)
	d.cmd(stCmdTxWithCRC)
	flags := d.waitIRQ(stIRQTxEnd, 3*time.Millisecond)
	d.sleep(time.Millisecond)
	return flags&stIRQTxEnd != 0
}

// activate runs the cascade (anticollision and select per level) after a
// WUPA response and returns UID and SAK.
func (d *stDriver) activate() ([]byte, byte, bool) {
	var uid []byte
	var sak byte
	for level := 1; level <= 3; level++ {
		ac, ok := d.anticollision(level)
		if !ok || stBCC(ac[:]) != 0 {
			return nil, 0, false
		}
		cascade := ac[0] == 0x88
		if cascade {
			uid = append(uid, ac[1:4]...)
		} else {
			uid = append(uid, ac[:4]...)
		}
		s, ok := d.selectLevel(level, ac)
		if !ok {
			return nil, 0, false
		}
		sak = s
		more := sak&0x04 != 0
		if cascade != more {
			return nil, 0, false
		}
		if !more {
			return uid, sak, true
		}
	}
	return nil, 0, false
}

func stBCC(b []byte) byte {
	var x byte
	for _, v := range b {
		x ^= v
	}
	return x
}

func (d *stDriver) pollNfcA() (stPollKind, *stNfcATag) {
	d.err = nil
	if !d.ready || !d.discovery {
		d.fail(errors.New("NFC-A discovery is not active"))
		return stInconclusive, nil
	}
	if d.nfcF {
		d.configureNfcA()
		d.nfcF, d.fieldReset, d.failedWakeups, d.noTags, d.noTagsF = false, false, 0, 0, 0
	}
	if d.fieldReset {
		d.resetField(false)
		d.fieldReset = false
	}
	w, atqa := d.requestWakeup()
	switch w {
	case stWakeNoTag:
		d.noTags++
		if d.noTags >= 3 {
			d.cached = nil
		}
		return stNoTag, nil
	case stWakeInconclusive:
		d.noTags, d.fieldReset = 0, true
		return stInconclusive, nil
	}
	d.noTags = 0
	uid, sak, ok := d.activate()
	if !ok {
		d.fieldReset = true
		return stInconclusive, nil
	}
	tag := &stNfcATag{ATQA: atqa, UID: uid, SAK: sak, TypeName: stTypeForSAK(sak)}
	if c := d.cached; c != nil && string(c.UID) == string(uid) {
		tag.TypeName, tag.Version, tag.Pages, tag.Memory = c.TypeName, c.Version, c.Pages, c.Memory
		tag.NDEF, tag.NDEFRead, tag.NDEFCap, tag.NDEFBytes = c.NDEF, c.NDEFRead, c.NDEFCap, c.NDEFBytes
	} else if sak == 0x00 {
		d.readType2(tag)
	}
	if d.err != nil {
		d.fieldReset = true
		return stInconclusive, nil
	}
	if !d.halt() {
		d.fieldReset = true
	}
	d.failedWakeups = 0
	d.cached = tag
	return stTag, tag
}

// ---- Type 2 (NTAG / Ultralight): read-only ----

// stType2Pages maps a GET_VERSION response to the tag's page count and name.
func stType2Pages(v []byte) (int, string) {
	if len(v) < 8 || v[1] != 0x04 {
		return 0, ""
	}
	pages := map[byte]int{0x0B: 20, 0x0E: 41, 0x0F: 45, 0x11: 135, 0x13: 231}[v[6]]
	switch v[2] {
	case 0x04:
		name := map[byte]string{0x0B: "NTAG210", 0x0E: "NTAG212", 0x0F: "NTAG213", 0x11: "NTAG215", 0x13: "NTAG216"}[v[6]]
		if name == "" {
			name = "NTAG"
		}
		return pages, name
	case 0x03:
		name := map[byte]string{0x0B: "MIFARE Ultralight EV1 (MF0UL11)", 0x0E: "MIFARE Ultralight EV1 (MF0UL21)"}[v[6]]
		if name == "" {
			name = "MIFARE Ultralight"
		}
		return pages, name
	}
	return pages, ""
}

func (d *stDriver) readType2(tag *stNfcATag) {
	// GET_VERSION identifies NTAG21x / Ultralight EV1. Older Ultralights
	// answer with a NAK and fall back to IDLE, so reactivate them.
	if v := d.transceive([]byte{0x60}, 8, 8, 5*time.Millisecond); len(v) == 8 {
		tag.Version = v
		pages, name := stType2Pages(v)
		tag.Pages = pages
		if name != "" {
			tag.TypeName = name
		}
	} else if d.err == nil {
		if w, _ := d.requestWakeup(); w != stWakeResponse {
			return
		}
		if uid, _, ok := d.activate(); !ok || string(uid) != string(tag.UID) {
			return
		}
	}
	limit := tag.Pages
	if limit == 0 {
		limit = stType2MaxPages
	}
	var mem []byte
	for page := 0; page < limit && d.err == nil; page += 4 {
		b := d.transceive([]byte{0x30, byte(page)}, 16, 16, 12*time.Millisecond)
		if len(b) != 16 {
			break
		}
		mem = append(mem, b...)
		if page == 0 && tag.Pages == 0 {
			// Ultralight without GET_VERSION: the capability container
			// gives the data area; READ rolls over at the end.
			if b[12] == 0xE1 {
				if pages := 4 + int(b[14])*2; b[14] == 0x06 {
					limit = pages
				} else {
					limit = min(stType2MaxPages, pages+16)
				}
			}
		}
	}
	if tag.Pages > 0 && len(mem) > tag.Pages*4 {
		mem = mem[:tag.Pages*4] // READ wraps around at the last page
	}
	tag.Memory = mem
	tag.NDEF, tag.NDEFRead, tag.NDEFCap, tag.NDEFBytes = stType2NDEF(mem)
}

// stType2NDEF reads the capability container (page 3) and the NDEF TLV from
// a Type 2 memory image: NDEF support, read access, data-area capacity and
// the NDEF message bytes.
func stType2NDEF(mem []byte) (ok, readable bool, capacity int, msg []byte) {
	if len(mem) < 16 {
		return
	}
	cc := mem[12:16]
	capacity = int(cc[2]) * 8
	if cc[0] != 0xE1 || cc[1]>>4 < 1 || capacity == 0 || capacity > 2040 {
		return false, false, 0, nil
	}
	ok, readable = true, cc[3]>>4 == 0
	if !readable {
		return
	}
	data := mem[16:]
	if len(data) > capacity {
		data = data[:capacity]
	}
	for i := 0; i < len(data); {
		t := data[i]
		i++
		if t == 0x00 {
			continue
		}
		if t == 0xFE || i >= len(data) {
			return
		}
		n := int(data[i])
		i++
		if n == 0xFF {
			if i+2 > len(data) {
				return
			}
			n = int(data[i])<<8 | int(data[i+1])
			i += 2
		}
		if n > len(data)-i {
			return
		}
		if t == 0x03 {
			return ok, readable, capacity, append([]byte(nil), data[i:i+n]...)
		}
		i += n
	}
	return
}

// ---- NFC-F ----

func (d *stDriver) pollNfcF() (stPollKind, *stNfcFTag) {
	d.err = nil
	if !d.ready || !d.discovery {
		d.fail(errors.New("NFC-F discovery is not active"))
		return stInconclusive, nil
	}
	if !d.nfcF {
		d.configureNfcF()
		d.nfcF, d.fieldReset, d.failedWakeups, d.noTagsF = true, false, 0, 0
	}
	if d.fieldReset {
		d.resetField(true)
		d.fieldReset = false
	}
	d.ensureField(true)
	d.prepareTransceive()
	d.setNoResponse(stNfcFPollTimeout)
	d.modify(stRegAux, 0, stAuxNoCRCRx)
	d.clearIRQs()
	d.cmd(stCmdClearFIFO)
	d.writeFIFO(stSensfReq)
	d.setTxLength(len(stSensfReq), 0)
	d.cmd(stCmdTxWithCRC)

	const irqs = stIRQTxEnd | stIRQRxStart | stIRQRxEnd | stIRQColl | stIRQNoResp | stIRQRxErrors
	const rxActivity = stIRQRxStart | stIRQRxEnd | stIRQColl | stIRQRxErrors
	start := time.Now()
	drain := start.Add(time.Millisecond)
	deadline := start.Add(stNfcFPollTimeout + 2*time.Millisecond)
	var flags uint32
	frameReady, window, txDone, fifo := false, false, false, 0
	inspect := func() bool {
		fifo = d.fifoSize()
		if fifo > len(stSensfReq) {
			window = true
		}
		if fifo >= stNfcFMinResponse {
			frameReady = true
		}
		return frameReady
	}
	for d.err == nil {
		now := time.Now()
		if (txDone || !now.Before(drain)) && inspect() {
			break
		}
		if !now.Before(deadline) {
			break
		}
		obs := d.waitIRQ(irqs, max(time.Until(deadline), time.Millisecond))
		flags |= obs
		txDone = txDone || obs&stIRQTxEnd != 0
		window = window || obs&rxActivity != 0
		if obs&(stIRQRxErrors|stIRQColl) != 0 {
			break
		}
		if (txDone || !time.Now().Before(drain)) && inspect() {
			break
		}
		if flags&stIRQRxEnd != 0 {
			break
		}
	}
	if d.err != nil {
		d.fieldReset = true
		return stInconclusive, nil
	}
	all := flags | d.stored
	fifo = max(fifo, d.fifoSize())
	edge := window || all&(stIRQRxStart|stIRQRxEnd) != 0
	if !frameReady && (edge || fifo > len(stSensfReq)) && all&(stIRQRxErrors|stIRQColl) == 0 {
		settle := time.Now().Add(3 * time.Millisecond)
		for !frameReady && time.Now().Before(settle) && d.err == nil {
			d.sleep(time.Millisecond)
			fifo = max(fifo, d.fifoSize())
			frameReady = fifo >= stNfcFMinResponse
		}
	}
	activity := window || fifo > len(stSensfReq) || all&rxActivity != 0
	if all&(stIRQRxErrors|stIRQColl) != 0 {
		d.fieldReset = true
		return stInconclusive, nil
	}
	if !frameReady {
		if all&stIRQNoResp != 0 && !activity || !activity && fifo <= len(stSensfReq) && (txDone || !time.Now().Before(deadline)) {
			d.noTagsF++
			return stNoTag, nil
		}
		d.fieldReset = true
		return stInconclusive, nil
	}
	for i := 0; i < 3 && d.fifoSize() < stNfcFMinResponse; i++ {
		d.sleep(time.Millisecond)
	}
	tag, ok := stParseSensfRes(d.readFIFO(stMaxFIFO))
	if !ok || d.err != nil {
		d.fieldReset = true
		return stInconclusive, nil
	}
	d.failedWakeups, d.noTagsF = 0, 0
	return stTag, tag
}

// stParseSensfRes decodes a SENSF_RES as the ST25R3916 FIFO holds it:
// length, response code 0x01, IDm (8), PMm (8), optional request data.
func stParseSensfRes(b []byte) (*stNfcFTag, bool) {
	if len(b) < stNfcFMinResponse || int(b[0]) < stNfcFMinResponse || b[0] > stNfcFMaxResponse || int(b[0]) > len(b) || b[1] != 0x01 {
		return nil, false
	}
	t := &stNfcFTag{}
	copy(t.IDm[:], b[2:10])
	copy(t.PMm[:], b[10:18])
	return t, true
}

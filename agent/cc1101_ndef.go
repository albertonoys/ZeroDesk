package main

import (
	"encoding/hex"
	"errors"
	"unicode/utf8"
)

// NDEF message parser, ported from CardputerZero/Cap-CC1101-NFC (MIT,
// src/nfc/ndef_parser.cpp): short and long records, Text (UTF-8) and URI
// well-known records, MIME records; chunked records are rejected like the
// original does.

type ndefRecord struct {
	Kind    string `json:"kind"` // text, uri, mime, unknown
	Type    string `json:"type"`
	Value   string `json:"value,omitempty"`
	Lang    string `json:"lang,omitempty"`
	Payload string `json:"payload"` // hex
}

var ndefURIPrefixes = [...]string{
	"", "http://www.", "https://www.", "http://", "https://", "tel:", "mailto:", "ftp://anonymous:anonymous@",
	"ftp://ftp.", "ftps://", "sftp://", "smb://", "nfs://", "ftp://", "dav://", "news:", "telnet://", "imap:",
	"rtsp://", "urn:", "pop:", "sip:", "sips:", "tftp:", "btspp://", "btl2cap://", "btgoep://", "tcpobex://",
	"irdaobex://", "file://", "urn:epc:id:", "urn:epc:tag:", "urn:epc:pat:", "urn:epc:raw:", "urn:epc:", "urn:nfc:",
}

func parseNDEF(b []byte) ([]ndefRecord, error) {
	if len(b) == 0 {
		return nil, errors.New("NDEF message is empty")
	}
	var out []ndefRecord
	first, end := true, false
	for i := 0; i < len(b); {
		if end {
			return nil, errors.New("data after the ME record")
		}
		h := b[i]
		i++
		mb, me, cf, sr, il, tnf := h&0x80 != 0, h&0x40 != 0, h&0x20 != 0, h&0x10 != 0, h&0x08 != 0, h&0x07
		if first != mb {
			return nil, errors.New("bad MB flag")
		}
		if cf {
			return nil, errors.New("chunked records are not supported")
		}
		if tnf == 0x06 || tnf == 0x07 {
			return nil, errors.New("invalid TNF")
		}
		if i >= len(b) {
			return nil, errors.New("truncated record header")
		}
		typeLen := int(b[i])
		i++
		var payLen int
		if sr {
			if i >= len(b) {
				return nil, errors.New("truncated payload length")
			}
			payLen = int(b[i])
			i++
		} else {
			if i+4 > len(b) {
				return nil, errors.New("truncated payload length")
			}
			n := uint32(b[i])<<24 | uint32(b[i+1])<<16 | uint32(b[i+2])<<8 | uint32(b[i+3])
			if n > uint32(len(b)) {
				return nil, errors.New("payload exceeds the message")
			}
			payLen = int(n)
			i += 4
		}
		idLen := 0
		if il {
			if i >= len(b) {
				return nil, errors.New("truncated ID length")
			}
			idLen = int(b[i])
			i++
		}
		if tnf == 0 && (typeLen != 0 || idLen != 0 || payLen != 0) {
			return nil, errors.New("empty record with content")
		}
		if tnf >= 1 && tnf <= 4 && typeLen == 0 {
			return nil, errors.New("typed record without type")
		}
		if tnf == 5 && typeLen != 0 {
			return nil, errors.New("unknown record with type")
		}
		if typeLen+idLen+payLen > len(b)-i {
			return nil, errors.New("record exceeds the message")
		}
		typ := b[i : i+typeLen]
		i += typeLen + idLen
		pay := b[i : i+payLen]
		i += payLen
		r, err := ndefDecode(tnf, typ, pay)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
		first, end = false, me
	}
	if !end {
		return nil, errors.New("missing ME record")
	}
	return out, nil
}

func ndefDecode(tnf byte, typ, pay []byte) (ndefRecord, error) {
	r := ndefRecord{Kind: "unknown", Type: string(typ), Payload: hex.EncodeToString(pay)}
	switch {
	case tnf == 1 && string(typ) == "T":
		if len(pay) == 0 {
			return r, errors.New("Text record without status byte")
		}
		st := pay[0]
		if st&0x40 != 0 {
			return r, errors.New("Text record uses the reserved bit")
		}
		ll := int(st & 0x3F)
		if ll > len(pay)-1 {
			return r, errors.New("Text record language exceeds payload")
		}
		if st&0x80 != 0 { // UTF-16: kept raw, as in the original
			return r, nil
		}
		text := pay[1+ll:]
		if !utf8.Valid(text) {
			return r, errors.New("Text record is not UTF-8")
		}
		r.Kind, r.Type, r.Value, r.Lang = "text", "text/plain", string(text), string(pay[1:1+ll])
	case tnf == 1 && string(typ) == "U":
		if len(pay) == 0 {
			return r, errors.New("URI record without identifier code")
		}
		prefix := ""
		if int(pay[0]) < len(ndefURIPrefixes) {
			prefix = ndefURIPrefixes[pay[0]]
		}
		r.Kind, r.Type, r.Value = "uri", "text/uri-list", prefix+string(pay[1:])
	case tnf == 2:
		r.Kind = "mime"
		if utf8.Valid(pay) {
			r.Value = string(pay)
		}
	}
	return r, nil
}

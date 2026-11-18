package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"unicode/utf8"
)

// Moqi's backend wire format is a little-endian uint32 length followed by
// ClientRequest or ServerResponse from proto/moqi.proto (upstream 13af5bc).
// Only the fields needed by this prototype are interpreted.
type request struct {
	seq, method, key, repeat uint32
	client                   string
	uiLess                   bool
	modifiers                []byte
}

type wireField struct {
	number, value uint64
	data          []byte
}

func fields(data []byte, visit func(wireField)) error {
	for len(data) != 0 {
		tag, n := binary.Uvarint(data)
		if n <= 0 || tag>>3 == 0 {
			return fmt.Errorf("invalid protobuf tag")
		}
		data = data[n:]
		f := wireField{number: tag >> 3}
		switch tag & 7 {
		case 0:
			f.value, n = binary.Uvarint(data)
			if n <= 0 {
				return fmt.Errorf("invalid protobuf integer")
			}
			data = data[n:]
		case 2:
			length, width := binary.Uvarint(data)
			if width <= 0 || length > uint64(len(data)-width) {
				return io.ErrUnexpectedEOF
			}
			f.data, data = data[width:width+int(length)], data[width+int(length):]
		case 1, 5:
			width := 8
			if tag&7 == 5 {
				width = 4
			}
			if len(data) < width {
				return io.ErrUnexpectedEOF
			}
			data = data[width:]
		default:
			return fmt.Errorf("unsupported protobuf wire type")
		}
		visit(f)
	}
	return nil
}

func parseRequest(data []byte) (r request, err error) {
	var key []byte
	err = fields(data, func(f wireField) {
		switch f.number {
		case 1:
			r.seq = uint32(f.value)
		case 2:
			r.method = uint32(f.value)
		case 6:
			r.uiLess = f.value != 0
		case 11:
			key = f.data
		case 23:
			r.client = string(f.data)
		}
	})
	if err != nil {
		return
	}
	if r.client == "" || !utf8.ValidString(r.client) {
		return r, fmt.Errorf("invalid Moqi client ID")
	}
	var packedErr error
	err = fields(key, func(f wireField) {
		switch f.number {
		case 2:
			r.key = uint32(f.value)
		case 3:
			r.repeat = uint32(f.value)
		case 6:
			if f.data == nil {
				r.modifiers = append(r.modifiers, byte(f.value))
				return
			}
			for data := f.data; len(data) > 0; {
				v, n := binary.Uvarint(data)
				if n <= 0 {
					packedErr = fmt.Errorf("invalid key state")
					break
				}
				r.modifiers = append(r.modifiers, byte(v))
				data = data[n:]
			}
		}
	})
	if err == nil {
		err = packedErr
	}
	return
}

func (r request) shortcut() bool {
	for _, key := range []int{0x11, 0x12, 0x5b, 0x5c} {
		if key < len(r.modifiers) && r.modifiers[key]&0x80 != 0 {
			return true
		}
	}
	return false
}

func response(r request, handled bool, commit string) []byte {
	data := make([]byte, 4, 64)
	var text = func(tag byte, value string) {
		data = append(data, tag)
		data = binary.AppendUvarint(data, uint64(len(value)))
		data = append(data, value...)
	}
	text(0x0a, r.client)
	data = binary.AppendUvarint(append(data, 0x10), uint64(r.seq))
	data = append(data, 0x18, 1)
	if handled {
		data = append(data, 0x20, 1)
	}
	if commit != "" {
		text(0x3a, commit)
	}
	// TSF may restore a closed keyboard compartment when switching profiles.
	// Open it on activation so Windows forwards ordinary key events.
	if r.method == 3 {
		data = binary.AppendUvarint(data, 22<<3)
		data = append(data, 1)
	}
	// show_candidates=false. Never supply candidates to the native presenter.
	binary.LittleEndian.PutUint32(data, uint32(len(data)-4))
	return data
}

type client struct {
	state             draft
	target            target
	uiLess, enterDown bool
	epoch             uint64
	serial            uint64
}

func serveMoqi(in io.Reader, out io.Writer, focusEpoch, displayed func() uint64, publish func(panelUpdate)) error {
	clients := make(map[string]*client)
	var visible string
	var serial uint64
	for {
		var size [4]byte
		if _, err := io.ReadFull(in, size[:]); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		n := binary.LittleEndian.Uint32(size[:])
		if n == 0 || n > 1<<20 {
			return fmt.Errorf("invalid Moqi frame length %d", n)
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(in, data); err != nil {
			return err
		}
		r, err := parseRequest(data)
		if err != nil {
			return err
		}
		var handled bool
		var commit string
		var update *panelUpdate
		c := clients[r.client]
		anchor := currentAnchor()
		epoch := focusEpoch()
		switch r.method {
		case 1: // init
			clients[r.client] = &client{uiLess: r.uiLess}
		case 2, 4, 14: // close, deactivate, composition terminated
			if c != nil {
				c.state = draft{}
				c.enterDown = false
			}
			if r.method == 2 {
				delete(clients, r.client)
			}
			if visible == r.client {
				visible = ""
				update = &panelUpdate{}
			}
		case 5, 6, 7, 8: // filter/handle down, filter/handle up
			if c == nil || c.uiLess || anchor.target.window == 0 {
				break
			}
			if c.epoch != epoch || c.target != anchor.target || (visible != "" && visible != r.client) {
				c.state, c.enterDown, c.target = draft{}, false, anchor.target
				c.epoch = epoch
			}
			if r.method == 7 || r.method == 8 {
				if r.key == keyEnter && c.enterDown {
					handled = true
					if r.method == 8 {
						c.enterDown = false
					}
				}
				break
			}
			handled = !r.shortcut() && (c.state.handles(r.key) || (r.key == keyEnter && c.enterDown))
			if r.method == 6 && handled {
				waitForPreview := r.key == keyEnter && c.state.Pinyin == "" && c.state.English != "" && c.serial != displayed()
				if !waitForPreview {
					commit = c.state.press(r.key, c.enterDown || r.repeat > 1)
					serial++
					c.serial = serial
					visible = r.client
					update = &panelUpdate{state: c.state, anchor: anchor, epoch: epoch, serial: serial}
				}
				if r.key == keyEnter {
					c.enterDown = true
				}
			} else if !handled && c.state.active() {
				c.state, c.enterDown = draft{}, false
				if visible == r.client {
					visible = ""
					update = &panelUpdate{}
				}
			}
		}
		if _, err := out.Write(response(r, handled, commit)); err != nil {
			return err
		}
		if update != nil {
			publish(*update)
		}
	}
}

//go:build unit

package common

import "testing"

func TestDecodeKeys(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want []keyKind
	}{
		{"up arrow", []byte{0x1b, '[', 'A'}, []keyKind{keyUp}},
		{"down arrow", []byte{0x1b, '[', 'B'}, []keyKind{keyDown}},
		{"unknown esc seq", []byte{0x1b, '[', 'C'}, []keyKind{keyOther}},
		{"enter cr", []byte{'\r'}, []keyKind{keyEnter}},
		{"ctrl-c", []byte{0x03}, []keyKind{keyCancel}},
		{"lone esc", []byte{0x1b}, []keyKind{keyCancel}},
		{"q cancels", []byte{'q'}, []keyKind{keyCancel}},
		{"digit", []byte{'3'}, []keyKind{keyDigit}},
		{"vim down/up", []byte{'j', 'k'}, []keyKind{keyDown, keyUp}},
		{"batched digits", []byte{'1', '2'}, []keyKind{keyDigit, keyDigit}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeKeys(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d events %v, want %d", len(got), got, len(tc.want))
			}
			for i := range got {
				if got[i].kind != tc.want[i] {
					t.Fatalf("event %d kind = %v, want %v", i, got[i].kind, tc.want[i])
				}
			}
		})
	}
}

func TestSelectorHandleKey(t *testing.T) {
	newSel := func() *selector { return &selector{items: []string{"a", "b", "c"}} }

	s := newSel()
	s.handleKey(keyEvent{kind: keyDown})
	if s.cursor != 1 {
		t.Fatalf("down: cursor=%d, want 1", s.cursor)
	}

	s = newSel()
	s.handleKey(keyEvent{kind: keyUp}) // wraps to last
	if s.cursor != 2 {
		t.Fatalf("up-wrap: cursor=%d, want 2", s.cursor)
	}

	s = newSel()
	s.handleKey(keyEvent{kind: keyDigit, digit: '3'})
	if s.cursor != 2 || s.typed != "3" {
		t.Fatalf("digit 3: cursor=%d typed=%q", s.cursor, s.typed)
	}

	s = newSel()
	s.handleKey(keyEvent{kind: keyDigit, digit: '9'}) // out of range (3 items)
	if s.cursor != 0 || s.typed != "" {
		t.Fatalf("out-of-range digit: cursor=%d typed=%q, want 0/empty", s.cursor, s.typed)
	}

	s = newSel()
	s.handleKey(keyEvent{kind: keyDigit, digit: '2'})
	s.handleKey(keyEvent{kind: keyDown}) // arrow clears the typed buffer
	if s.typed != "" {
		t.Fatalf("arrow should clear typed buffer, got %q", s.typed)
	}

	s = newSel()
	s.handleKey(keyEvent{kind: keyDown})
	done, ok := s.handleKey(keyEvent{kind: keyEnter})
	if !done || !ok || s.cursor != 1 {
		t.Fatalf("enter: done=%v ok=%v cursor=%d", done, ok, s.cursor)
	}

	s = newSel()
	if done, ok := s.handleKey(keyEvent{kind: keyCancel}); !done || ok {
		t.Fatalf("cancel: done=%v ok=%v, want done/!ok", done, ok)
	}
}

func TestSelectorMultiDigit(t *testing.T) {
	s := &selector{items: make([]string, 12)}

	s.handleKey(keyEvent{kind: keyDigit, digit: '1'})
	if s.cursor != 0 {
		t.Fatalf("after '1': cursor=%d, want 0", s.cursor)
	}
	s.handleKey(keyEvent{kind: keyDigit, digit: '2'}) // "12" → item 12
	if s.cursor != 11 || s.typed != "12" {
		t.Fatalf("after '12': cursor=%d typed=%q", s.cursor, s.typed)
	}
	s.handleKey(keyEvent{kind: keyBackspace}) // back to "1" → item 1
	if s.typed != "1" || s.cursor != 0 {
		t.Fatalf("after backspace: cursor=%d typed=%q", s.cursor, s.typed)
	}
}

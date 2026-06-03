package scrollregion

import (
	"strings"
	"testing"
)

func TestGuardWritesEnableThenDisable(t *testing.T) {
	var buf strings.Builder
	restore := Guard(&buf)
	if buf.String() != EnableSeq() {
		t.Fatalf("Guard did not write the enable sequence; got %q", buf.String())
	}
	restore()
	if buf.String() != EnableSeq()+DisableSeq() {
		t.Fatalf("after restore, got %q want enable+disable", buf.String())
	}
}

func TestSequencesAreDECSET1007(t *testing.T) {
	if EnableSeq() != "\x1b[?1007h" {
		t.Errorf("EnableSeq=%q", EnableSeq())
	}
	if DisableSeq() != "\x1b[?1007l" {
		t.Errorf("DisableSeq=%q", DisableSeq())
	}
}

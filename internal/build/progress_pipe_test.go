package build

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

func TestBuildProgressFilterPipeWritesOneStatusLine(t *testing.T) {
	var out bytes.Buffer
	f := newBuildProgressFilter(&out, "probe")
	if !f.toPipe {
		t.Fatal("a bytes.Buffer is not a terminal; the filter must treat it as a pipe")
	}
	var in strings.Builder
	for i := 1; i <= 300; i++ {
		in.WriteString("[")
		in.WriteString(strconv.Itoa(i))
		in.WriteString("/300] Building CXX object CMakeFiles/mitiru_host.dir/x.cpp.obj\r\n")
	}
	if _, err := f.Write([]byte(in.String())); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := f.Finish(true); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "\r") {
		t.Errorf("pipe output must not use carriage returns: %q", got)
	}
	if got != "[300/300] engine:300 user:0\n" {
		t.Errorf("want one final status line, got %q", got)
	}
}

package phpworker

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadResultSkipsBlankLines(t *testing.T) {
	worker := &Worker{
		stdout: bufio.NewReader(strings.NewReader("\n{\"id\":\"job-1\",\"ok\":true}\n")),
	}

	result, err := worker.readResult()
	if err != nil {
		t.Fatalf("readResult returned error: %v", err)
	}
	if result.ID != "job-1" {
		t.Fatalf("unexpected result id: %s", result.ID)
	}
	if !result.OK {
		t.Fatalf("expected result ok")
	}
}

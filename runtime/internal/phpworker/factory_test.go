package phpworker

import (
	"os"
	"testing"

	"github.com/roadrunner-server/goridge/v3/pkg/frame"
	"github.com/roadrunner-server/goridge/v3/pkg/pipe"
)

func TestReadResultReadsGoridgeJSONFrame(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	defer pr.Close()
	defer pw.Close()

	relay := pipe.NewPipeRelay(pr, pw)
	payload := []byte("{\"id\":\"job-1\",\"ok\":true}")
	fr := frame.NewFrame()
	fr.WriteVersion(fr.Header(), frame.Version1)
	fr.WriteFlags(fr.Header(), frame.CodecJSON)
	fr.WritePayloadLen(fr.Header(), uint32(len(payload)))
	fr.WritePayload(payload)
	fr.WriteCRC(fr.Header())

	if err = relay.Send(fr); err != nil {
		t.Fatalf("send frame: %v", err)
	}

	worker := &Worker{relay: relay}
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

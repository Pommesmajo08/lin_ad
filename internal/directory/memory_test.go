package directory

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func memoryFixture() State {
	now := time.Now().UTC()
	exit := 0
	st := State{NextUnixID: 60002, Users: map[string]User{"admin": {Name: "admin", Admin: true}}, Groups: map[string][]string{"staff": {"admin"}}, Devices: map[string]Device{"d": {ID: "d", OS: "linux", AllowedUsers: []string{"admin"}, AllowedGroups: []string{"staff"}, LastJobPoll: &now}}, Jobs: map[string]Job{"j": {ID: "j", DeviceIDs: []string{"d"}, NextRunAt: &now}}, Runs: map[string]JobRun{}}
	for i := 0; i < MaxJobRuns; i++ {
		id := fmt.Sprint(i)
		st.Runs[id] = JobRun{ID: id, JobID: "j", DeviceID: "d", Status: "succeeded", Script: strings.Repeat("s", 16000) + id, Output: strings.Repeat("o", MaxJobOutput-8) + id, StartedAt: &now, FinishedAt: &now, LeaseUntil: &now, ExitCode: &exit}
	}
	return st
}

var memoryCopySink State

func BenchmarkDirectorySnapshot(b *testing.B) {
	st := memoryFixture()
	b.Run("JSONRoundTrip", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			data, e := json.Marshal(st)
			if e != nil {
				b.Fatal(e)
			}
			var next State
			if e = json.Unmarshal(data, &next); e != nil {
				b.Fatal(e)
			}
			memoryCopySink = next
		}
	})
	b.Run("Snapshot", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			memoryCopySink = copyJobState(st)
		}
	})
}

func TestSnapshotIsolation(t *testing.T) {
	st := memoryFixture()
	snapshot := copyJobState(st)
	if !reflect.DeepEqual(st, snapshot) {
		t.Fatal("snapshot changed directory values")
	}
	original := copyJobState(st)
	snapshot.Groups["staff"][0] = "changed"
	d := snapshot.Devices["d"]
	d.AllowedUsers[0] = "changed"
	d.AllowedGroups[0] = "changed"
	*d.LastJobPoll = time.Time{}
	j := snapshot.Jobs["j"]
	j.DeviceIDs[0] = "changed"
	*j.NextRunAt = time.Time{}
	r := snapshot.Runs["0"]
	*r.ExitCode = 12
	*r.StartedAt = time.Time{}
	*r.FinishedAt = time.Time{}
	*r.LeaseUntil = time.Time{}
	delete(snapshot.Runs, "1")
	delete(snapshot.Users, "admin")
	delete(snapshot.Devices, "d")
	delete(snapshot.Jobs, "j")
	if !reflect.DeepEqual(st, original) {
		t.Fatal("snapshot mutation changed committed state")
	}
}

// Exercise the real idle polling route with full-sized history. Bound allocation
// traffic separately from live heap growth, so a JSON-copy regression fails early.
func TestIdlePollingMemoryBounded(t *testing.T) {
	st := memoryFixture()
	secret := strings.Repeat("a", 64)
	d := st.Devices["d"]
	d.CredentialHash = tokenKey(secret)
	st.Devices["d"] = d
	s := &Server{state: st, path: t.TempDir() + "/directory.json"}
	handler := s.Handler()
	poll := func() {
		req := httptest.NewRequest("POST", "/v1/machine/jobs/claim", strings.NewReader(`{"request_id":"`+strings.Repeat("b", 64)+`"}`))
		req.Header.Set("Authorization", "Device d."+secret)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != 200 {
			t.Fatalf("poll failed: %d %s", response.Code, response.Body.String())
		}
	}
	poll()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 8 {
		poll()
	}
	runtime.ReadMemStats(&after)
	perPoll := (after.TotalAlloc - before.TotalAlloc) / 8
	if perPoll > 8<<20 {
		t.Fatalf("idle poll allocates %d bytes with full history", perPoll)
	}
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&before)
	for batch := range 5 {
		for range 100 {
			poll()
		}
		runtime.GC()
		runtime.GC()
		runtime.ReadMemStats(&after)
		t.Logf("polls=%d heap=%.2f MiB allocation/poll=%.2f MiB", (batch+1)*100, float64(after.HeapAlloc)/(1<<20), float64(perPoll)/(1<<20))
	}
	if after.HeapAlloc > before.HeapAlloc+32<<20 {
		t.Fatalf("idle polling retained over 32 MiB: before=%d after=%d", before.HeapAlloc, after.HeapAlloc)
	}
	runtime.KeepAlive(s)
}

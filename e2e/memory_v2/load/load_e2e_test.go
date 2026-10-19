//go:build memory_e2e

package load

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rest "github.com/Tencent/WeKnora/e2e/memory_v2/e2eclient"
	"github.com/Tencent/WeKnora/e2e/memory_v2/mcpclient"
	"github.com/stretchr/testify/require"
)

// Pre-declared gates (README "Scale and soak"). They are constants on purpose:
// a failed run is investigated and fixed, never re-scored with new numbers.
const (
	readP95Ms, readP99Ms     = 1000.0, 2000.0
	modelP95Ms               = 15000.0
	maxUnexpectedFailureRate = 0.001
	readRatePerSecond        = 10
	drainDeadline            = 10 * time.Minute
	trendTolerance           = 1.25
)

func envInt(name string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return fallback
}

type resourcePoint struct {
	At         time.Time `json:"at"`
	RSSKiB     float64   `json:"rss_kib"`
	PGConns    float64   `json:"pg_connections"`
	Goroutines float64   `json:"goroutines"`
	HeapInuse  float64   `json:"heap_inuse_bytes"`
}

type loadReport struct {
	RunID, Rows, Hardware string
	Phases                map[string]map[string]Summary
	Resources             []resourcePoint
	Violations            []string
	Passed                bool
}

func sql(t *testing.T, container, statement string) string {
	t.Helper()
	out, err := exec.Command("docker", "exec", container, "psql", "-X", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "weknora_e2e", "-tAc", statement).Output()
	require.NoError(t, err, statement)
	return strings.TrimSpace(string(out))
}

// TestScaleAndSoak is opt-in and sequential. Seeding uses real bge-m3 vectors and
// the production atomic create transaction; the measured traffic then goes
// through the public REST/MCP surfaces, including model-backed recall and save.
func TestScaleAndSoak(t *testing.T) {
	if os.Getenv("MEMV2_E2E_LOAD") != "1" {
		t.Skip("set MEMV2_E2E_LOAD=1 for the isolated scale/soak gate")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	world, err := rest.LoadIsolatedWorld(ctx)
	require.NoError(t, err)
	rows := envInt("MEMV2_E2E_LOAD_ROWS", 2000)
	soak := time.Duration(envInt("MEMV2_E2E_LOAD_SOAK_MINUTES", 0)) * time.Minute
	phase := time.Duration(envInt("MEMV2_E2E_LOAD_PHASE_SECONDS", 60)) * time.Second
	container := "memv2-e2e-pg"
	if world.BaseURL == "http://localhost:18082" {
		container = "memv2-release-gate-postgres-1"
	}

	fixture, err := NewFixture(ctx, world)
	require.NoError(t, err)
	report := &loadReport{RunID: fixture.RunID, Rows: strconv.Itoa(rows), Phases: map[string]map[string]Summary{},
		Hardware: fmt.Sprintf("%s/%s cpus=%d", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())}
	_, source, _, _ := runtime.Caller(0)
	out := filepath.Join(filepath.Dir(source), "..", ".run", "load-"+fixture.RunID+".json")
	require.NoError(t, os.MkdirAll(filepath.Dir(out), 0o700))
	flush := func() {
		data, err := json.MarshalIndent(report, "", "  ")
		require.NoError(t, err, "the load report must always be serializable")
		require.NoError(t, os.WriteFile(out, data, 0o600))
	}
	var saved sync.Map // id -> profile index for ACK verification
	grantedAdmin := false
	t.Cleanup(func() {
		flush()
		if grantedAdmin {
			sql(t, container, fmt.Sprintf("UPDATE users SET is_system_admin=false WHERE email='%s'", fixture.Profiles[0].Email))
		}
		// Scoped strictly to this run's fixture KBs.
		for _, p := range fixture.Profiles {
			sql(t, container, fmt.Sprintf(`DELETE FROM memory_relations WHERE from_uuid IN (SELECT id FROM agent_memories WHERE kb_id='%s')`, p.KBID))
			sql(t, container, fmt.Sprintf(`DELETE FROM extraction_queue WHERE kb_id='%s'`, p.KBID))
			sql(t, container, fmt.Sprintf(`DELETE FROM agent_memories_audit WHERE kb_id='%s'`, p.KBID))
			sql(t, container, fmt.Sprintf(`DELETE FROM agent_memories WHERE kb_id='%s'`, p.KBID))
			cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
			_ = p.Client.Do(cleanup, http.MethodDelete, "/api/v1/knowledge-bases/"+p.KBID, nil, nil)
			stop()
		}
		_ = fixture.Close()
	})
	t.Logf("scale fixture run=%s rows=%d report=%s", fixture.RunID, rows, out)
	require.NoError(t, fixture.SeedTo(ctx, rows))

	// Resource sampling: process RSS, PG connections and (SystemAdmin, synthetic
	// load user only) content-free runtime metrics.
	require.True(t, strings.HasPrefix(fixture.Profiles[0].Email, "load-"))
	sql(t, container, fmt.Sprintf("UPDATE users SET is_system_admin=true WHERE email='%s' AND email LIKE 'load-%%@memv2-e2e.test'", fixture.Profiles[0].Email))
	grantedAdmin = true
	pidRaw, _ := os.ReadFile(filepath.Join(filepath.Dir(source), "..", ".run", "app.pid"))
	sample := func() resourcePoint {
		point := resourcePoint{At: time.Now()}
		if world.BaseURL == "http://localhost:18080" {
			if b, err := exec.Command("ps", "-o", "rss=", "-p", strings.TrimSpace(string(pidRaw))).Output(); err == nil {
				point.RSSKiB, _ = strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
			}
		}
		point.PGConns, _ = strconv.ParseFloat(sql(t, container, "SELECT count(*) FROM pg_stat_activity WHERE datname='weknora_e2e'"), 64)
		req, _ := http.NewRequestWithContext(ctx, "GET", world.BaseURL+"/api/v1/memories/metrics", nil)
		session, err := rest.Login(ctx, world.BaseURL, fixture.Profiles[0].Email, "Memv2-e2e!")
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+session.Token)
			if resp, err := http.DefaultClient.Do(req); err == nil {
				data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
				resp.Body.Close()
				for _, line := range strings.Split(string(data), "\n") {
					fields := strings.Fields(line)
					if len(fields) != 2 {
						continue
					}
					switch fields[0] {
					case "weknora_memory_runtime_goroutines":
						point.Goroutines, _ = strconv.ParseFloat(fields[1], 64)
					case "weknora_memory_runtime_heap_inuse_bytes":
						point.HeapInuse, _ = strconv.ParseFloat(fields[1], 64)
					}
				}
			}
		}
		return point
	}
	var resMu sync.Mutex
	samplerCtx, stopSampler := context.WithCancel(ctx)
	samplerDone := make(chan struct{})
	go func() {
		defer close(samplerDone)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			point := sample()
			resMu.Lock()
			report.Resources = append(report.Resources, point)
			resMu.Unlock()
			select {
			case <-samplerCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	// ---- read traffic without model calls at a fixed open-loop rate ----
	readRecorder := &Recorder{}
	runReads := func(duration time.Duration) {
		ticker := time.NewTicker(time.Second / readRatePerSecond)
		defer ticker.Stop()
		deadline := time.After(duration)
		inflight := make(chan struct{}, 128)
		var wg sync.WaitGroup
		for {
			select {
			case <-deadline:
				wg.Wait()
				return
			case <-ticker.C:
				select {
				case inflight <- struct{}{}:
				default:
					readRecorder.Add(Sample{Op: "read", Failed: true, Status: 0}) // saturation is a failure, not a skipped sample
					continue
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() { <-inflight }()
					p := fixture.Profiles[rand.IntN(len(fixture.Profiles))]
					row := p.Rows[rand.IntN(len(p.Rows))]
					path := "/api/v1/memories?" + url.Values{"kb_id": {p.KBID}, "page": {strconv.Itoa(1 + rand.IntN(5))}, "page_size": {"20"}}.Encode()
					switch rand.IntN(4) {
					case 1:
						path = "/api/v1/memories/" + row.ID
					case 2:
						path = "/api/v1/memories/stats?kb_id=" + p.KBID
					case 3:
						path = "/api/v1/memories/graph?kb_id=" + p.KBID + "&limit=30"
					}
					started := time.Now()
					err := p.Client.Do(ctx, http.MethodGet, path, nil, nil)
					s := Sample{Op: "read", Duration: time.Since(started), Status: rest.StatusOf(err), Failed: err != nil, Err: errClass(err)}
					readRecorder.Add(s)
					// Isolation probe: another tenant's memory must never be readable.
					if rand.IntN(5) == 0 {
						other := fixture.Profiles[(rand.IntN(2)+1+indexOf(fixture, p))%len(fixture.Profiles)]
						foreign := other.Rows[rand.IntN(len(other.Rows))]
						probeErr := p.Client.Do(ctx, http.MethodGet, "/api/v1/memories/"+foreign.ID, nil, nil)
						status := rest.StatusOf(probeErr)
						readRecorder.Add(Sample{Op: "isolation", Status: status, Expected: status == 403 || status == 404, Failed: status != 403 && status != 404})
					}
				}()
			}
		}
	}

	// ---- model-backed MCP traffic at 1/10/25/50 sessions with think time ----
	runModelPhase := func(sessions int, duration time.Duration) *Recorder {
		recorder := &Recorder{}
		var wg sync.WaitGroup
		deadline := time.Now().Add(duration)
		for i := 0; i < sessions; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				p := fixture.Profiles[i%len(fixture.Profiles)]
				session, err := mcpclient.Connect(ctx, world.BaseURL, p.Endpoint.Path, p.Endpoint.Token)
				if err != nil {
					recorder.Add(Sample{Op: "connect", Failed: true, Err: errClass(err)})
					return
				}
				defer session.Close()
				for n := 0; time.Now().Before(deadline); n++ {
					time.Sleep(time.Duration(1500+rand.IntN(1500)) * time.Millisecond)
					started := time.Now()
					if n%5 == 4 {
						content := fmt.Sprintf("Load session %s-%d-%d logged a unique calibration value %d at tick %d.", fixture.RunID[:8], sessions, i, rand.Int64(), n)
						res, err := session.Save(ctx, p.KBID, content)
						sample := Sample{Op: "model_save", Duration: time.Since(started), Failed: err != nil, Err: errClass(err)}
						if err == nil {
							saved.Store(res.Memory.ID, indexOf(fixture, p))
						}
						recorder.Add(sample)
						continue
					}
					p0 := p.Rows[rand.IntN(len(p.Rows))]
					_, err := session.Recall(ctx, p.KBID, firstWords(p0.Content, 8), 5)
					recorder.Add(Sample{Op: "model_recall", Duration: time.Since(started), Failed: err != nil, Err: errClass(err)})
				}
			}(i)
		}
		wg.Wait()
		return recorder
	}

	var violations []string
	check := func(ok bool, format string, args ...any) {
		if !ok {
			violations = append(violations, fmt.Sprintf(format, args...))
		}
	}
	runReads(phase)
	for _, sessions := range []int{1, 10, 25, 50} {
		var done atomic.Bool
		go func() { // reads continue while model sessions run, as in production
			for !done.Load() {
				runReads(10 * time.Second)
			}
		}()
		recorder := runModelPhase(sessions, phase)
		done.Store(true)
		summary := Summarize(recorder.Snapshot())
		report.Phases[fmt.Sprintf("sessions_%d", sessions)] = summary
		for op, s := range summary {
			check(!math.IsNaN(s.P95Ms) && s.P95Ms <= modelP95Ms, "%d sessions %s p95 %.0fms > %.0fms", sessions, op, s.P95Ms, modelP95Ms)
			check(s.FailureRate <= maxUnexpectedFailureRate, "%d sessions %s failure rate %.4f", sessions, op, s.FailureRate)
		}
		flush()
	}
	if soak > 0 {
		soakEnd := time.Now().Add(soak)
		for time.Now().Before(soakEnd) {
			var done atomic.Bool
			go func() {
				for !done.Load() {
					runReads(10 * time.Second)
				}
			}()
			recorder := runModelPhase(25, min(time.Until(soakEnd), 5*time.Minute))
			done.Store(true)
			report.Phases["soak_"+time.Now().Format("150405")] = Summarize(recorder.Snapshot())
			flush()
		}
	}
	stopSampler()
	<-samplerDone
	readSummary := Summarize(readRecorder.Snapshot())
	report.Phases["reads"] = readSummary
	reads, isolation := readSummary["read"], readSummary["isolation"]
	check(reads.Count > 0 && reads.P95Ms <= readP95Ms && reads.P99Ms <= readP99Ms, "non-model reads p95/p99 %.0f/%.0fms", reads.P95Ms, reads.P99Ms)
	check(reads.FailureRate <= maxUnexpectedFailureRate, "non-model read failure rate %.4f", reads.FailureRate)
	check(isolation.Failures == 0 && isolation.ExpectedPolicy > 0, "cross-tenant isolation probes failed=%d expected=%d", isolation.Failures, isolation.ExpectedPolicy)

	// ---- integrity after load ----
	deadline := time.Now().Add(drainDeadline)
	for {
		pending, _ := strconv.Atoi(sql(t, container, fmt.Sprintf(`SELECT count(*) FROM extraction_queue WHERE job_type='graph_link' AND kb_id IN ('%s','%s','%s') AND status IN ('pending','processing')`, fixture.Profiles[0].KBID, fixture.Profiles[1].KBID, fixture.Profiles[2].KBID)))
		if pending == 0 || time.Now().After(deadline) {
			check(pending == 0, "graph queue did not drain within %s (%d pending)", drainDeadline, pending)
			break
		}
		time.Sleep(5 * time.Second)
	}
	kbList := fmt.Sprintf("'%s','%s','%s'", fixture.Profiles[0].KBID, fixture.Profiles[1].KBID, fixture.Profiles[2].KBID)
	check(sql(t, container, `SELECT count(*) FROM extraction_queue WHERE status='failed' AND kb_id IN (`+kbList+`)`) == "0", "terminal graph failures remain")
	check(sql(t, container, `SELECT count(*) FROM memory_relations r JOIN agent_memories a ON a.id=r.from_uuid JOIN agent_memories b ON b.id=r.to_uuid WHERE r.deleted_at IS NULL AND (a.tenant_id<>b.tenant_id OR a.kb_id<>b.kb_id OR a.deleted_at IS NOT NULL OR b.deleted_at IS NOT NULL) AND a.kb_id IN (`+kbList+`)`) == "0", "invalid graph edges")
	for _, p := range fixture.Profiles {
		// Protected rows (index 0/1) must be unchanged: same verdict and tier.
		// GORM's default:2 stores an inserted zero tier as 2, so the fixture's protection
		// is its verdict; content, verdict and liveness must be unchanged.
		state := sql(t, container, fmt.Sprintf(`SELECT string_agg(verdict||'/'||(deleted_at IS NOT NULL)::text, ',' ORDER BY verdict) FROM agent_memories WHERE id IN ('%s','%s')`, p.Rows[0].ID, p.Rows[1].ID))
		check(state == "decision/false,fixed/false", "protected rows changed for %s: %s", p.KBID, state)
		check(sql(t, container, fmt.Sprintf(`SELECT count(*) FROM agent_memories WHERE (id='%s' AND content=$$%s$$) OR (id='%s' AND content=$$%s$$)`, p.Rows[0].ID, p.Rows[0].Content, p.Rows[1].ID, p.Rows[1].Content)) == "2", "protected content changed for %s", p.KBID)
	}
	var acked, missing int
	saved.Range(func(key, value any) bool {
		acked++
		p := fixture.Profiles[value.(int)]
		if err := p.Client.Do(ctx, http.MethodGet, "/api/v1/memories/"+key.(string), nil, nil); err != nil {
			missing++
		}
		return true
	})
	check(missing == 0, "%d of %d acknowledged saves were not retrievable", missing, acked)
	createdAudits, _ := strconv.Atoi(sql(t, container, `SELECT count(*) FROM agent_memories_audit WHERE action='created' AND actor<>'e2e_fixture' AND kb_id IN (`+kbList+`)`))
	check(createdAudits >= acked, "audit rows %d are fewer than acknowledged saves %d", createdAudits, acked)

	// ---- resource trend: compare the last third with the middle third ----
	resMu.Lock()
	points := append([]resourcePoint(nil), report.Resources...)
	resMu.Unlock()
	if len(points) >= 9 {
		third := len(points) / 3
		trend := func(name string, pick func(resourcePoint) float64, slack float64) {
			mid, last := []float64{}, []float64{}
			for _, p := range points[third : 2*third] {
				if v := pick(p); !math.IsNaN(v) && v > 0 {
					mid = append(mid, v)
				}
			}
			for _, p := range points[2*third:] {
				if v := pick(p); !math.IsNaN(v) && v > 0 {
					last = append(last, v)
				}
			}
			if len(mid) > 0 && len(last) > 0 {
				check(Median(last) <= Median(mid)*trendTolerance+slack, "%s trend grew from %.0f to %.0f", name, Median(mid), Median(last))
			}
		}
		trend("rss", func(p resourcePoint) float64 { return p.RSSKiB }, 0)
		trend("goroutines", func(p resourcePoint) float64 { return p.Goroutines }, 20)
		trend("heap_inuse", func(p resourcePoint) float64 { return p.HeapInuse }, 0)
		trend("pg_connections", func(p resourcePoint) float64 { return p.PGConns }, 5)
	} else {
		violations = append(violations, "too few resource samples to assess trend; extend duration")
	}
	report.Violations = violations
	report.Passed = len(violations) == 0
	flush()
	require.Empty(t, violations, "pre-declared load gates failed; thresholds were not changed")
}

func indexOf(f *Fixture, p *Profile) int {
	for i, candidate := range f.Profiles {
		if candidate == p {
			return i
		}
	}
	return 0
}

func firstWords(s string, n int) string {
	words := strings.Fields(s)
	return strings.Join(words[:min(n, len(words))], " ")
}

// errClass keeps only a coarse class so failures can be attributed without
// copying request content, tokens or model text into the report.
func errClass(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}
	var toolErr *mcpclient.ToolError
	if errors.As(err, &toolErr) {
		return "tool_error"
	}
	if status := rest.StatusOf(err); status != 0 {
		return "http_" + strconv.Itoa(status)
	}
	return "transport"
}

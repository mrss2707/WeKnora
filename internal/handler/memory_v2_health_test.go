package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type healthReportServiceFake struct {
	*memoryV2HandlerServiceFake
	report *types.HealthReport
	err    error
	calls  int
}

func (f *healthReportServiceFake) AssessHealthReport(_ context.Context, tenantID, kbID string) (*types.HealthReport, error) {
	f.calls++
	f.healthTenant, f.healthKbID = tenantID, kbID
	return f.report, f.err
}

func TestHealthReportPreservesCoverage(t *testing.T) {
	svc := &healthReportServiceFake{
		memoryV2HandlerServiceFake: &memoryV2HandlerServiceFake{},
		report: &types.HealthReport{TenantID: "42", Coverage: &types.MemoryHealthCoverage{
			Total: 2000, Scanned: 1000, Limit: 1000, Reasons: []string{"scan_limit"},
		}},
	}
	h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
	c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/health?kb_id=kb1", nil)
	setMemoryV2Auth(c, 42, "")
	h.GetHealthReport(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, svc.calls)
	require.Zero(t, svc.healthCalls, "optional report must not run a second scan through the legacy method")
	require.Equal(t, "42", svc.healthTenant)
	require.Equal(t, "kb1", svc.healthKbID)
	coverage := jsonMap(t, jsonMap(t, decodeJSONBody(t, recorder)["data"])["coverage"])
	require.Equal(t, false, coverage["complete"])
	require.EqualValues(t, 2000, coverage["total"])
	require.EqualValues(t, 1000, coverage["scanned"])
}

func TestHealthReportLegacyCoverageUnknown(t *testing.T) {
	h := NewMemoryV2Handler(&memoryV2HandlerServiceFake{}, &memoryV2HandlerRepoFake{}, nil, nil)
	c, recorder := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/health", nil)
	setMemoryV2Auth(c, 42, "")
	h.GetHealthReport(c)
	coverage := jsonMap(t, jsonMap(t, decodeJSONBody(t, recorder)["data"])["coverage"])
	require.Equal(t, false, coverage["complete"])
	require.Contains(t, coverage["reasons"], "coverage_unknown")
}

func TestHealthReportErrorsDoNotLeakDetails(t *testing.T) {
	svc := &healthReportServiceFake{memoryV2HandlerServiceFake: &memoryV2HandlerServiceFake{}, err: errors.New("private SQL connection detail")}
	h := NewMemoryV2Handler(svc, &memoryV2HandlerRepoFake{}, nil, nil)
	c, _ := newMemoryV2HandlerTestContext(http.MethodGet, "/memories/health", nil)
	setMemoryV2Auth(c, 42, "")
	h.GetHealthReport(c)
	require.Len(t, c.Errors, 1)
	require.NotContains(t, c.Errors.Last().Error(), "private SQL")
	c, _ = newMemoryV2HandlerTestContext(http.MethodGet, "/memories/health", nil)
	h.GetHealthReport(c)
	require.Equal(t, 1, svc.calls, "missing auth must not reach the optional interface")
}

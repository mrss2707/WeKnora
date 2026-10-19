package workers

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/unicode/norm"
)

// These labels precede the evaluation. The corpus covers lexical assertions,
// not general semantic inference or cross-language translation.
func TestHealthAssertionCorpus(t *testing.T) {
	type pair struct {
		a, b     string
		opposite bool
	}
	pairs := []pair{
		{"The sky is blue", "The sky is not blue", true},
		{"The cache is enabled", "The cache is not enabled", true},
		{"The worker can retry jobs", "The worker cannot retry jobs", true},
		{"The queue is durable", "The queue isn't durable", true},
		{"The proxy does forward requests", "The proxy doesn't forward requests", true},
		{"The workers do preserve metadata", "The workers don’t preserve metadata", true},
		{"The endpoint will accept requests", "The endpoint won't accept requests", true},
		{"The audit log has recorded changes", "The audit log hasn't recorded changes", true},
		{"The gateways are reachable", "The gateways aren't reachable", true},
		{"The restored database was available", "The restored database wasn't available", true},
		{"The clients should send headers", "The clients shouldn't send headers", true},
		{"The handler could expose credentials", "The handler couldn't expose credentials", true},
		{"The application stores documents", "The application never stores documents", true},
		{"The service keeps no credentials", "The service keeps credentials", true},
		{"The node is stale", "The node is not stale", true},
		{"Máy chủ lưu nhật ký", "Máy chủ không lưu nhật ký", true},
		{"Bộ đệm hoạt động", "Bộ đệm không hoạt động", true},
		{"Hàng đợi giữ dữ liệu", "Hàng đợi không giữ dữ liệu", true},
		{"Dịch vụ hỗ trợ tiếng Việt", "Dịch vụ không hỗ trợ tiếng Việt", true},
		{"Người dùng được tạo tài liệu", "Người dùng không được tạo tài liệu", true},
		{"Bản sao lưu chứa dữ liệu", "Bản sao lưu không chứa dữ liệu", true},
		{"Ứng dụng hiển thị cảnh báo", "Ứng dụng không hiển thị cảnh báo", true},
		{"Tác vụ xử lý yêu cầu", "Tác vụ không xử lý yêu cầu", true},
		{"Cơ sở dữ liệu sử dụng mã hóa", "Cơ sở dữ liệu không sử dụng mã hóa", true},
		{"Tài khoản quản trị được sửa cấu hình", "Tài khoản quản trị không được sửa cấu hình", true},
		{"Máy chủ ghi lại lỗi", "Máy chủ chẳng ghi lại lỗi", true},
		{"Hệ thống giữ nguyên thẻ", "Hệ thống không giữ nguyên thẻ", true},
		{"Mô hình tạo đề xuất", "Mô hình không tạo đề xuất", true},
		{"Người xem đọc được tài liệu", "Người xem không đọc được tài liệu", true},
		{"Bộ dựng nhúng phông chữ", "Bộ dựng không nhúng phông chữ", true},
		{"Alice likes tea", "Bob does not like tea", false},
		{"The sky is blue", "The grass is not green", false},
		{"The server is enabled", "The server is enabled", false},
		{"The cache is not enabled", "The cache is not enabled", false},
		{"The notebook stores text", "The notebook stores text", false},
		{"The server is enabled and reachable", "The server is not enabled and reachable", false},
		{"The server is enabled if configured", "The server is not enabled if configured", false},
		{"The cache is only enabled", "The cache is not only enabled", false},
		{"Maybe the server is enabled", "Maybe the server is not enabled", false},
		{"Is the server enabled?", "Is the server not enabled?", false},
		{"The server is enabled; the cache is active", "The server is not enabled; the cache is active", false},
		{"The cache is not disabled", "The cache is enabled", false},
		{"The server is not never enabled", "The server is enabled", false},
		{"The frontend uses Node and npm", "The PDF renderer does not embed fonts", false},
		{"The cache is disabled", "The cache is not enabled", false},
		{"Máy chủ lưu nhật ký", "Bộ đệm không lưu nhật ký", false},
		{"Bộ dựng nhúng phông chữ tiếng Việt", "The frontend builds with Node 22 and pnpm is not supported; use npm ci", false},
		{"Máy chủ lưu nhật ký", "Máy chủ lưu nhật ký", false},
		{"Máy chủ không lưu nhật ký", "Máy chủ không lưu nhật ký", false},
		{"Máy chủ lưu nhật ký và dữ liệu", "Máy chủ không lưu nhật ký và dữ liệu", false},
		{"Máy chủ lưu nhật ký nếu được cấu hình", "Máy chủ không lưu nhật ký nếu được cấu hình", false},
		{"Máy chủ chỉ lưu nhật ký", "Máy chủ không chỉ lưu nhật ký", false},
		{"Có lẽ máy chủ lưu nhật ký", "Có lẽ máy chủ không lưu nhật ký", false},
		{"Máy chủ chưa lưu nhật ký", "Máy chủ lưu nhật ký", false},
		{"Máy chủ lưu nhật ký?", "Máy chủ không lưu nhật ký?", false},
		{"Máy chủ lưu nhật ký; bộ đệm hoạt động", "Máy chủ không lưu nhật ký; bộ đệm hoạt động", false},
		{"Máy chủ không chẳng lưu nhật ký", "Máy chủ lưu nhật ký", false},
		{"Máy chủ lưu lỗi", "Máy chủ không lưu dữ liệu", false},
		{"节点保持数据", "节点不保持数据", false},
		{"The client is active", "The client is not active because configured", false},
	}
	truePositive, falsePositive, positives, samples := 0, 0, 0, 0
	for _, variant := range []string{"plain", "unicode"} {
		for i, tc := range pairs {
			a, b := tc.a, tc.b
			if variant == "unicode" {
				a = norm.NFD.String(strings.ToUpper(a))
				b = norm.NFD.String(b) + "."
			}
			memory := func(id, content string) *types.AgentMemory {
				return &types.AgentMemory{ID: id, TenantID: "tenant", KbID: "kb", Content: content}
			}
			issues, _, err := contradictionCandidates(context.Background(), []*types.AgentMemory{memory("a", a), memory("b", b)})
			require.NoError(t, err)
			predicted := len(issues) > 0
			if tc.opposite {
				positives++
				if predicted {
					truePositive++
				}
			} else if predicted {
				falsePositive++
			}
			samples++
			assert.Equal(t, tc.opposite, predicted, "case %d/%s: %q vs %q", i, variant, a, b)
		}
	}
	require.GreaterOrEqual(t, samples, 100)
	precision := float64(truePositive) / float64(max(1, truePositive+falsePositive))
	recall := float64(truePositive) / float64(positives)
	require.GreaterOrEqual(t, precision, .95)
	require.GreaterOrEqual(t, recall, .80)
	t.Logf("lexical corpus: %d samples, precision %.3f, recall %.3f", samples, precision, recall)
}

func TestHealthCandidatesScopeAndCancellation(t *testing.T) {
	makeMemory := func(id, tenant, kb, content string) *types.AgentMemory {
		return &types.AgentMemory{ID: id, TenantID: tenant, KbID: kb, Content: content}
	}
	for _, foreign := range []*types.AgentMemory{
		makeMemory("b", "other", "kb", "The cache is not enabled"),
		makeMemory("b", "tenant", "other", "The cache is not enabled"),
	} {
		issues, _, err := contradictionCandidates(context.Background(), []*types.AgentMemory{
			makeMemory("a", "tenant", "kb", "The cache is enabled"), foreign,
		})
		require.NoError(t, err)
		require.Empty(t, issues)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := contradictionCandidates(ctx, []*types.AgentMemory{makeMemory("a", "tenant", "kb", "The cache is enabled")})
	require.ErrorIs(t, err, context.Canceled)
}

func TestHealthReportCoverageAndBudget(t *testing.T) {
	repo := &mockHealthCheckerRepo{searchFunc: func(ctx context.Context, filter *types.MemoryFilter) ([]*types.MemorySearchResult, int64, error) {
		require.Equal(t, healthScanLimit, filter.Limit)
		var rows []*types.MemorySearchResult
		for i := 0; i < healthScanLimit; i++ {
			rows = append(rows, hcSearchResult(freshMemory(fmt.Sprint(i), "tenant", fmt.Sprintf("Record %d contains data", i), nil, 3, 0)))
		}
		return rows, 2000, nil
	}}
	report, err := NewHealthChecker(repo).AssessHealth(context.Background(), "tenant", "", 0)
	require.NoError(t, err)
	require.EqualValues(t, 2000, report.Coverage.Total)
	require.Equal(t, 1000, report.Coverage.Scanned)
	require.False(t, report.Coverage.Complete)
	require.Contains(t, report.Coverage.Reasons, "scan_limit")
	require.Contains(t, report.Coverage.Reasons, "issue_limit")
	require.Len(t, report.Issues, healthIssueLimit)
	require.Equal(t, "unavailable", report.Checks["embedding_dimension"].Status)
}

func TestHealthRecentAccessAndDuplicateScope(t *testing.T) {
	h := NewHealthChecker(nil)
	memory := freshMemory("a", "tenant", "The cache stores a private access token", []string{"cache"}, 0, 2)
	memory.CreatedAt = time.Now().Add(-200 * 24 * time.Hour)
	now := time.Now()
	memory.LastAccessedAt = &now
	require.Empty(t, h.checkStaleFacts([]*types.AgentMemory{memory}))
	other := *memory
	other.ID = "b"
	require.Len(t, h.checkDuplications([]*types.AgentMemory{memory, &other}), 1)
	other.KbID = "other"
	require.Empty(t, h.checkDuplications([]*types.AgentMemory{memory, &other}))
}

func TestNextHealthRunIsDaily(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	for _, now := range []time.Time{
		time.Date(2026, 10, 18, 3, 59, 0, 0, time.UTC),
		time.Date(2026, 10, 18, 4, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 18, 4, 1, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 4, 1, 0, 0, zone),
	} {
		next := nextHealthRun(now)
		require.True(t, next.After(now))
		require.Equal(t, 4, next.Hour())
		require.LessOrEqual(t, next.Sub(now), 25*time.Hour)
	}
}

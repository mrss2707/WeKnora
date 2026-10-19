package quality

import (
	"fmt"
	"strings"
)

var holdoutTopics = []string{
	"clock synchronizer", "federation bridge", "account provisioner", "message sequencer", "routing catalog",
	"signature inspector", "thumbnail producer", "allocation planner", "manifest publisher", "telemetry relay",
	"segment uploader", "document normalizer", "membership resolver", "replication observer", "delivery tracker",
	"connection broker", "timeline reader", "redaction filter", "archive verifier", "pagination cursor",
	"recovery planner", "certificate registry", "event recorder", "content classifier", "storage adapter",
	"task journal", "lease catalog", "counter exporter", "permission registry", "chunk manifest",
	"connection supervisor", "checksum ledger", "deployment observer", "index inspector", "policy compiler",
	"signal collector", "query planner", "image converter", "bundle verifier", "workspace observer",
	"cursor validator", "transport adapter", "result formatter", "key distributor", "revision tracker",
	"storage coordinator", "upload notifier", "artifact collector", "search manifest", "audit synchronizer",
}

// HoldoutCorpus freezes new subjects and independently worded assertions before
// the revised prompt is evaluated. It is a bounded task corpus, not a claim of
// arbitrary production-domain reasoning quality.
func HoldoutCorpus() []Case {
	cases := Corpus()
	for i := range cases {
		tc := &cases[i]
		topic := holdoutTopics[i/4]
		tc.ID = "holdout-" + tc.ID
		tc.Content = strings.ReplaceAll(tc.Content, topics[i/4], topic)
		tc.MergeContent = strings.ReplaceAll(tc.MergeContent, topics[i/4], topic)
		if tc.Positive {
			switch tc.Kind {
			case "wip":
				tc.Content = fmt.Sprintf("Engineering is actively implementing %s. The remaining implementation work has not finished yet.", topic)
				if tc.Language == "vi" {
					tc.Content = fmt.Sprintf("Nhóm kỹ thuật vẫn đang xây dựng %s. Công việc triển khai còn lại đang được thực hiện và chưa hoàn thành.", topic)
				}
			case "gotcha":
				tc.Content = fmt.Sprintf("The %s loses pending writes on a zero timeout without reporting an error. This reproducible data-loss defect is an operational trap.", topic)
				if tc.Language == "vi" {
					tc.Content = fmt.Sprintf("Thành phần %s làm mất các bản ghi đang chờ khi thời gian chờ bằng không mà không báo lỗi. Lỗi mất dữ liệu tái hiện được này là cạm bẫy vận hành.", topic)
				}
			case "importance":
				tc.Content = fmt.Sprintf("Preserving the %s reference is mandatory: it is essential to avoid irreversible loss of stored records during disaster recovery.", topic)
				if tc.Language == "vi" {
					tc.Content = fmt.Sprintf("Bắt buộc giữ tài liệu tham chiếu của %s: đây là thông tin thiết yếu để tránh mất dữ liệu không thể khôi phục trong quá trình phục hồi thảm họa.", topic)
				}
			}
		} else if tc.Verdict == "none" {
			if i/4%4 == 2 {
				tc.Content = fmt.Sprintf("The %s records UTC timestamps using RFC3339 notation.", topic)
				if tc.Language == "vi" {
					tc.Content = fmt.Sprintf("Thành phần %s ghi thời điểm theo múi giờ UTC bằng định dạng RFC3339.", topic)
				}
			} else {
				tc.Content = fmt.Sprintf("No reliable evidence has established anything about %s. There is no confirmed fault and no assigned work item.", topic)
				if tc.Language == "vi" {
					tc.Content = fmt.Sprintf("Chưa có bằng chứng đáng tin cậy về %s. Chưa xác nhận lỗi hay giao bất kỳ công việc nào.", topic)
				}
			}
		}
	}
	return cases
}

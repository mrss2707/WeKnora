package load

import "fmt"

// FixtureContent gives every seeded fact a distinct subject/value. Templates
// alternate EN/VI and several domains; semantic ingestion dedup is deliberately
// not part of fixture construction.
func FixtureContent(profile, index int) string {
	subject := fmt.Sprintf("station-%d-%05d", profile, index)
	value := 10 + index%997
	switch index % 10 {
	case 0:
		return fmt.Sprintf("Station %s retains immutable database snapshots for %d days under its documented archival policy.", subject, value)
	case 1:
		return fmt.Sprintf("Trạm %s ghi lại chỉ số nhiệt độ mỗi %d giây vào nhật ký giám sát riêng.", subject, value)
	case 2:
		return fmt.Sprintf("The %s manufacturing line checks a copper reference gauge every %d finished components.", subject, value)
	case 3:
		return fmt.Sprintf("Kho %s đặt giới hạn %d kiện hàng cho từng kệ chứa được kiểm định.", subject, value)
	case 4:
		return fmt.Sprintf("Observatory %s records %d infrared samples before running its nightly prism calibration.", subject, value)
	case 5:
		return fmt.Sprintf("Tuyến %s dành %d phút cho việc kiểm tra tín hiệu trước mỗi ca vận hành.", subject, value)
	case 6:
		return fmt.Sprintf("The %s botanical archive preserves %d seed envelopes in a humidity-controlled cabinet.", subject, value)
	case 7:
		return fmt.Sprintf("Bộ dựng %s dùng mức phân giải %d điểm cho sơ đồ kỹ thuật được xuất hàng tuần.", subject, value)
	case 8:
		return fmt.Sprintf("The %s document registry keeps %d revisions before starting a new archival volume.", subject, value)
	default:
		return fmt.Sprintf("Thư viện %s kiểm kê %d đầu sách trong mỗi đợt đối soát dữ liệu định kỳ.", subject, value)
	}
}

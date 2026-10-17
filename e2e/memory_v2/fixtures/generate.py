#!/usr/bin/env python3
"""Generates memories.json, the Memory V2 end-to-end fixture set.

Knowledge bases (see seed): acme-eng (shared editor), acme-hr (shared viewer),
beta-ops (Beta tenant), gamma-private (Gamma, unshared isolation canary).

Every memory has a stable key; queries reference keys. Re-run after edits:
    python3 e2e/memory_v2/fixtures/generate.py
"""
import json
import os

HERE = os.path.dirname(os.path.abspath(__file__))
EVAL = os.path.join(HERE, '../../../internal/application/service/memory_v2/eval/testdata/recall_eval.json')

CANARY = 'GAMMA-CANARY-7f3a'

ENG_EXTRA = [
    ('deploy_friday', 'Deploying on Friday afternoons broke the release pipeline twice because nobody was on call; releases now freeze at 3pm on Fridays.'),
    ('staging_nodes', 'The staging Kubernetes cluster runs on three nodes in ap-southeast-1 with autoscaling disabled.'),
    ('prod_nodes', 'Production runs on nine nodes across three availability zones; the cluster autoscaler may add up to six more.'),
    ('feature_flags', 'Feature flags are evaluated server-side with Unleash; frontend flags are read from the /api/v1/flags endpoint.'),
    ('jwt_rotation', 'JWT signing keys rotate every 30 days; old keys stay valid for 7 days so existing sessions survive the rotation.'),
    ('rate_limit', 'The public API rate limit is 120 writes per minute per user and 600 reads per minute per user.'),
    ('log_retention', 'Application logs are kept for 14 days in Loki; audit logs are kept for one year in cold storage.'),
    ('backup_schedule', 'Postgres is backed up with pgBackRest: a full backup every Sunday and incremental backups every six hours.'),
    ('restore_drill', 'The last restore drill took 42 minutes for a 300 GB database; the target recovery time is under one hour.'),
    ('ci_cache', 'CI caches Go modules and the npm store per branch; a cold build takes 11 minutes and a warm build 4 minutes.'),
    ('flaky_test', 'TestStreamManager_Concurrent is flaky under -race on CI runners with two CPUs; it is quarantined behind a build tag.'),
    ('ocr_fallback', 'Scanned PDFs without a text layer fall back to PaddleOCR; Vietnamese diacritics need the latin_vi model.'),
    ('chunk_size', 'Documents are chunked at 512 tokens with 64 tokens of overlap; tables are kept in one chunk when possible.'),
    ('sso_oidc', 'Single sign-on uses OIDC with Keycloak; the realm is weknora and the client id is weknora-web.'),
    ('cors_origins', 'Allowed CORS origins are configured through CORS_ALLOWED_ORIGINS; wildcards are rejected in production.'),
    ('ssrf_whitelist', 'Outbound model calls go through the SSRF guard; local model hosts must be listed in SSRF_WHITELIST.'),
    ('neo4j_optional', 'Neo4j is optional; without it the knowledge graph features are hidden in the UI.'),
    ('node_version', 'The frontend builds with Node 22 and pnpm is not supported; use npm ci.'),
    ('i18n_locales', 'The web UI ships six locales: zh-CN, en-US, ko-KR, vi-VN, ru-RU and ja-JP; zh-CN is the source locale.'),
    ('oncall_rotation', 'On-call rotates weekly on Monday at 10:00; the handover checklist lives in the runbooks repository.'),
    ('pager_escalation', 'Pages escalate to the secondary on-call after 15 minutes without acknowledgement.'),
    ('incident_0412', 'Incident 2026-04-12: an expired TLS certificate on the API gateway caused a 37-minute outage.'),
    ('cert_renewal', 'TLS certificates are renewed by cert-manager 30 days before expiry; alerts fire at 14 days.'),
    ('cdn_cache', 'Static assets are served through the CDN with a one-year cache; index.html is never cached.'),
    # Vietnamese engineering memories
    ('vi_deploy_flow', 'Quy trình triển khai: build image, chạy migrate trên staging, kiểm tra smoke test rồi mới promote lên production.'),
    ('vi_backup_kms', 'Bản sao lưu cơ sở dữ liệu được mã hoá bằng khoá KMS riêng và lưu ở vùng khác với cụm chính.'),
    ('vi_oncall', 'Người trực ca phải xác nhận cảnh báo trong vòng 15 phút, nếu không cảnh báo sẽ chuyển cho người trực phụ.'),
    ('vi_rate_limit', 'Giới hạn ghi của API công khai là 120 lần mỗi phút cho mỗi người dùng.'),
    ('vi_logs', 'Nhật ký ứng dụng được giữ 14 ngày; nhật ký kiểm toán được giữ một năm.'),
    ('vi_ocr_model', 'Tài liệu PDF dạng ảnh scan được xử lý bằng PaddleOCR, cần mô hình latin_vi để giữ dấu tiếng Việt.'),
    ('vi_staging', 'Cụm staging có ba node ở ap-southeast-1 và không bật tự động mở rộng.'),
    ('vi_flags', 'Cờ tính năng được đánh giá phía máy chủ bằng Unleash.'),
    ('vi_cert', 'Chứng chỉ TLS được cert-manager gia hạn trước 30 ngày; cảnh báo bật khi còn 14 ngày.'),
    ('vi_minio', 'Tệp tải lên được lưu trong bucket MinIO weknora-files có bật versioning.'),
]

# Contradiction pair: the newer memory refutes the older one.
CONTRADICTIONS = [
    ('es_primary', 'Elasticsearch is our primary search engine for document retrieval.'),
    ('es_decommissioned', 'The Elasticsearch cluster was decommissioned in March 2026; Postgres with ParadeDB replaced it for search.'),
]

# Designated near-duplicates: the second restates the first and is expected
# to merge into it on save (cosine >= 0.93 with bge-m3).
NEAR_DUPLICATES = [
    ('dup_redis_a', 'Redis is only used as the asynq queue backend and never as a cache.'),
    ('dup_redis_b', 'Redis is used only as the asynq queue backend, never as a cache.'),
]

LONG_VI = ('Biên bản họp kiến trúc quý 3: ' + ' '.join(
    f'Mục {i}: nhóm đã rà soát lại cấu hình kết nối cơ sở dữ liệu, giới hạn tài nguyên của dịch vụ và quy trình sao lưu, '
    f'thống nhất giữ nguyên các tham số hiện tại cho đến đợt kiểm tra tải tiếp theo.' for i in range(1, 60)))[:9000]

HR = [
    ('hr_leave', 'Employees get 18 days of paid annual leave; unused days carry over until the end of March.'),
    ('hr_remote', 'Remote work is allowed up to three days per week; Tuesdays and Thursdays are office days.'),
    ('hr_probation', 'The probation period is two months for engineers and one month for interns.'),
    ('hr_expenses', 'Expenses under 2,000,000 VND are approved by the team lead; larger ones need the finance manager.'),
    ('hr_laptop', 'New hires receive a MacBook Pro on their first day; the laptop is returned within five days of leaving.'),
    ('hr_review', 'Performance reviews happen twice a year, in June and December.'),
    ('hr_bonus', 'The annual bonus is paid in January together with the thirteenth-month salary.'),
    ('hr_insurance', 'Health insurance covers employees and up to two dependants from the first month.'),
    ('hr_training', 'Each employee has a training budget of 10,000,000 VND per year for courses and conferences.'),
    ('hr_overtime', 'Overtime must be approved in advance and is compensated at 150% on weekdays and 200% on weekends.'),
    ('hr_vi_nghi_phep', 'Nhân viên có 18 ngày nghỉ phép năm có lương; ngày chưa dùng được chuyển sang đến hết tháng Ba.'),
    ('hr_vi_lam_tu_xa', 'Được làm việc từ xa tối đa ba ngày mỗi tuần; thứ Ba và thứ Năm làm tại văn phòng.'),
    ('hr_vi_thu_viec', 'Thời gian thử việc là hai tháng với kỹ sư và một tháng với thực tập sinh.'),
    ('hr_vi_danh_gia', 'Đánh giá hiệu suất diễn ra hai lần mỗi năm vào tháng Sáu và tháng Mười Hai.'),
    ('hr_vi_thuong', 'Thưởng năm được trả vào tháng Một cùng lương tháng mười ba.'),
    ('hr_vi_bao_hiem', 'Bảo hiểm sức khoẻ áp dụng cho nhân viên và tối đa hai người phụ thuộc ngay từ tháng đầu.'),
    ('hr_vi_dao_tao', 'Mỗi nhân viên có ngân sách đào tạo 10 triệu đồng mỗi năm cho khoá học và hội thảo.'),
    ('hr_vi_tang_ca', 'Làm thêm giờ phải được duyệt trước, tính 150% ngày thường và 200% cuối tuần.'),
    ('hr_onboarding', 'Onboarding takes one week: day one is accounts and laptop, day two security training, then a buddy project.'),
    ('hr_offboarding', 'On the last day, IT revokes access at 18:00 and the manager collects the badge.'),
]

BETA = [
    ('beta_region', 'Beta runs its workloads in eu-central-1 and keeps backups in eu-west-1.'),
    ('beta_oncall', 'Beta on-call uses Opsgenie with a two-person rotation that changes every Wednesday.'),
    ('beta_db', 'Beta uses MySQL 8 for billing and Postgres 16 for analytics.'),
    ('beta_release', 'Beta releases every second Tuesday after the change advisory board meeting.'),
    ('beta_sla', 'Beta guarantees 99.9% monthly availability to its enterprise customers.'),
    ('beta_vi_khu_vuc', 'Hệ thống của Beta chạy ở eu-central-1 và sao lưu ở eu-west-1.'),
    ('beta_vi_phat_hanh', 'Beta phát hành hai tuần một lần vào thứ Ba sau cuộc họp hội đồng thay đổi.'),
    ('beta_monitoring', 'Beta monitors services with Prometheus and Grafana; alerts route to the #beta-alerts channel.'),
]

GAMMA_FACTS = [
    'the Gamma vault code is stored in a hardware security module in Hanoi',
    'Gamma payroll runs on the 25th of each month through an internal batch job',
    'the Gamma board meets every quarter in the Da Nang office',
    'Gamma customer contracts are archived for ten years in encrypted cold storage',
    'Gamma uses an air-gapped build server for its firmware releases',
    'the Gamma incident hotline is staffed around the clock by two engineers',
    'Gamma marketing launches are announced only after legal review',
    'Gamma laptops are reimaged every six months by the security team',
    'Gamma keeps its source code in a self-hosted Gitea instance',
    'the Gamma office Wi-Fi password changes on the first Monday of each month',
]
GAMMA = [(f'gamma_{i + 1}', f'{CANARY} {fact}.') for i, fact in enumerate(GAMMA_FACTS)]


def main():
    with open(EVAL, encoding='utf-8') as f:
        ev = json.load(f)
    kbs = {
        'acme-eng': [{'key': m['key'], 'content': m['content']} for m in ev['memories']]
        + [{'key': k, 'content': c} for k, c in ENG_EXTRA + CONTRADICTIONS + NEAR_DUPLICATES]
        + [{'key': 'vi_long_minutes', 'content': LONG_VI}],
        'acme-hr': [{'key': k, 'content': c} for k, c in HR],
        'beta-ops': [{'key': k, 'content': c} for k, c in BETA],
        'gamma-private': [{'key': k, 'content': c} for k, c in GAMMA],
    }
    with open(os.path.join(HERE, 'extra_memories.json'), encoding='utf-8') as f:
        for kb, additions in json.load(f).items():
            kbs[kb].extend({'key': k, 'content': c} for k, c in additions)
    for kb, memories in kbs.items():
        assert len({m['key'] for m in memories}) == len(memories), f'duplicate keys in {kb}'
    queries = [
        {'kb': 'acme-eng', **q} for q in ev['queries']
    ] + [
        {'kb': 'acme-eng', 'q': 'when are releases frozen on friday', 'want': ['deploy_friday'], 'kind': 'en-paraphrase'},
        {'kb': 'acme-eng', 'q': 'how often do jwt keys rotate', 'want': ['jwt_rotation'], 'kind': 'en-keyword'},
        {'kb': 'acme-eng', 'q': 'người trực ca phải xác nhận cảnh báo trong bao lâu', 'want': ['vi_oncall', 'pager_escalation'], 'kind': 'vi'},
        {'kb': 'acme-eng', 'q': 'giới hạn số lần ghi API mỗi phút', 'want': ['vi_rate_limit', 'rate_limit'], 'kind': 'vi'},
        {'kb': 'acme-eng', 'q': 'what replaced elasticsearch', 'want': ['es_decommissioned'], 'kind': 'en-paraphrase'},
        {'kb': 'acme-hr', 'q': 'how many days of annual leave', 'want': ['hr_leave', 'hr_vi_nghi_phep'], 'kind': 'en-keyword'},
        {'kb': 'acme-hr', 'q': 'ngày làm việc tại văn phòng', 'want': ['hr_vi_lam_tu_xa', 'hr_remote'], 'kind': 'vi'},
        {'kb': 'acme-hr', 'q': 'when is the bonus paid', 'want': ['hr_bonus', 'hr_vi_thuong'], 'kind': 'en-paraphrase'},
        {'kb': 'beta-ops', 'q': 'which region does beta run in', 'want': ['beta_region', 'beta_vi_khu_vuc'], 'kind': 'en-paraphrase'},
    ]
    out = {
        'canary': CANARY,
        'near_duplicates': [[NEAR_DUPLICATES[0][0], NEAR_DUPLICATES[1][0]]],
        'contradictions': [[CONTRADICTIONS[0][0], CONTRADICTIONS[1][0]]],
        'knowledge_bases': kbs,
        'queries': queries,
    }
    with open(os.path.join(HERE, 'memories.json'), 'w', encoding='utf-8') as f:
        json.dump(out, f, ensure_ascii=False, indent=1)
        f.write('\n')
    total = sum(len(v) for v in kbs.values())
    print(f'{total} memories, {len(queries)} queries')


if __name__ == '__main__':
    main()

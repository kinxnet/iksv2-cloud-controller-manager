#!/usr/bin/env bash
# GitHub 라벨 일괄 생성 스크립트
# 사전 조건: gh CLI 설치 및 인증 완료 (gh auth login)
# 사용법: bash .github/scripts/setup-labels.sh

set -euo pipefail

REPO="${GH_REPO:-$(gh repo view --json nameWithOwner -q .nameWithOwner)}"

create_label() {
  local name="$1"
  local color="$2"
  local description="$3"

  if gh label create "$name" \
       --repo "$REPO" \
       --color "$color" \
       --description "$description" 2>/dev/null; then
    echo "  created: $name"
  else
    gh label edit "$name" \
      --repo "$REPO" \
      --color "$color" \
      --description "$description"
    echo "  updated: $name"
  fi
}

echo "=== 워크플로우 라벨 ==="
create_label "draft-by-human"    "0075ca" "사람이 직접 작성한 이슈 초안"
create_label "draft-by-agent"    "cfd3d7" "Agent가 작성한 이슈 초안 (사람 검토 필요)"
create_label "ready-for-agent"   "0e8a16" "Agent 작업 시작 승인 완료"
create_label "agent-wip"         "fbca04" "Agent 작업 진행 중"
create_label "needs-revision"    "e4e669" "Agent 결과물 수정 필요"
create_label "needs-human-lead"  "b60205" "사람 주도 필요 (high-risk 또는 복잡도 높은 영역)"

echo ""
echo "=== 작업 유형 라벨 ==="
create_label "type/feature"       "0075ca" "신규 기능 추가"
create_label "type/refactor"      "0075ca" "기존 코드 리팩터링"
create_label "type/bugfix"        "d73a4a" "버그 수정"
create_label "type/test"          "0075ca" "테스트 작성/보강"
create_label "type/docs"          "0075ca" "문서 작성/업데이트"
create_label "type/infra"         "0075ca" "인프라/설정 변경"
create_label "type/investigation" "0075ca" "분석/조사"

echo ""
echo "=== 위험도 라벨 ==="
create_label "risk/low"    "0e8a16" "단독 기능, 테넌트 격리 무관"
create_label "risk/medium" "fbca04" "공유 컴포넌트 수정"
create_label "risk/high"   "b60205" "인증/인가, 외부 노출 API, 데이터 변경, 공유 인프라 — blast radius 큼"

echo ""
echo "=== 도메인 라벨 (시스템 계층 기반 메타 도메인 10종) ==="
create_label "domain/api"            "1d76db" "외부/내부 노출 HTTP·gRPC 엔드포인트"
create_label "domain/control-plane"  "5319e7" "상태 reconcile·오케스트레이션·워크플로우"
create_label "domain/data-plane"     "0e8a16" "실시간 트래픽·이벤트 처리 경로"
create_label "domain/storage"        "c5def5" "DB·캐시·객체 스토리지·마이그레이션"
create_label "domain/auth"           "b60205" "인증·인가·시크릿·세션"
create_label "domain/integration"    "fbca04" "외부 SaaS·CSP·결제 등 연동"
create_label "domain/observability"  "bfd4f2" "로깅·메트릭·트레이싱·알림"
create_label "domain/infra"          "0075ca" "빌드·배포·CI/CD·IaC"
create_label "domain/sdk-cli"        "cfd3d7" "클라이언트 라이브러리·CLI"
create_label "domain/docs"           "d4c5f9" "문서·튜토리얼"

echo ""
echo "=== 우선순위 라벨 ==="
create_label "priority/p0" "b60205" "즉시 대응 (인시던트)"
create_label "priority/p1" "e4e669" "이번 스프린트"
create_label "priority/p2" "0075ca" "다음 스프린트"
create_label "priority/p3" "cfd3d7" "백로그"

echo ""
echo "✅ 라벨 설정 완료 (repo: $REPO)"

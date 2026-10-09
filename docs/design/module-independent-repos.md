---
title: "모듈 독립 레포 전환 — 설계와 작업 분담"
doc_type: "design"
scope: "project"
target: "terra-agent"
status: "active"
version: "v1.0"
last_updated: "2026-10-09"
---

# 모듈 독립 레포 전환 — 설계와 작업 분담

> 이 문서는 네 레포(`Terra` · `modules` · `terra-sdk` · `terra-agent`)에 **같은 내용**으로 놓인다.
> 각 레포의 세션은 자기 레포의 절(§5)을 맡고, 나머지는 경계를 이해하기 위해 읽는다.
> 본문은 2026-10-09 결정과 그날 코드 조사(Terra `origin/main`, `modules` `origin/main` ca7d648)에 근거한다.

## 1. 결정 (확정)

| ID | 결정 |
| --- | --- |
| D-1 | 플랫폼 소유 모듈 6개(`io.terra.scene-runtime` · `player` · `scene-studio` · `scene.terra` · `ext.metrics` · `virtual-device`)는 Terra `module/`에 **잔류**한다. |
| D-2 | 모듈이 쓰는 Go SDK는 **공개 얇은 SDK** `StellaxiaLab/terra-sdk`로 새로 정의한다. 코어 내부 패키지를 그대로 공개하지 않는다. |
| D-3 | **원본은 공개 SDK 한 곳**이다. Terra의 `modulert` 등은 `type X = sdk.X` 별칭으로 바꾼다(호출부 무변경). 코어가 자기 복사본을 따로 유지하지 않는다. |
| D-4 | Go 모듈 경로는 `github.com/StellaxiaLab/...`. |
| D-14 | **C안** — 1단계(공개 SDK + modules 독립 빌드)를 먼저 하고, 공개 마켓(퍼블리셔 계정·전역 서명·공개 카탈로그)은 외부 퍼블리셔 수요가 생긴 뒤에 착수한다. **1단계에서 마켓 관련 작업(서명 체계, 퍼블리셔 심사, 공개 접근, 마켓 화면)은 하지 않는다.** |
| D-15 | `terra-agent-core`(1,591줄)는 별도 공개 레포 `StellaxiaLab/terra-agent`로 분리한다. `terra-cli`와 `io.terra.agent` 모듈이 같이 쓴다. |
| — | `terra-io-weave`(512줄)는 코어 사용처가 없으므로 `io.terra.io-weave` 모듈 **안으로** 옮긴다. |

**왜 이렇게 되었나** — Terra는 비공개, `modules`는 공개다. SDK는 `terra-module-runtime`(7,460줄)·`terra-svi`를 `../` 상대경로 replace로 요구해서 `modules` 단독으로는 빌드되지 않고, 모듈의 `go.mod` 경로가 `github.com/terra-project/terra/module/...`로 조직명도 위치도 맞지 않는다(자기 경로를 import하는 파일 49개). 그래서 "SDK를 게시"하는 것이 아니라 **공개해도 되는 약속만 새로 모은 얇은 패키지**를 만든다.

**하지 않는 것(되돌리지 말 것)** — tree 레지스트리의 기록된 설계는 "전역 단일 마켓플레이스를 버리고 enroll이 신원을 대신한다"이다(`Terra:docs/ideas/module-distribution-tree-registry-ideas.md`). 공개 마켓은 이 결정을 뒤집는 일이므로 사용자 결정 없이 시작하지 않는다.

## 2. 레포와 의존 방향

```text
terra-sdk   (공개 · 외부 의존 0)
   ▲
   │        terra-agent  (공개 · 외부 의존 0 목표, P-1)
   │           ▲      ▲
modules (공개) ─┘      │      ← modules 는 terra-sdk · terra-agent 를 require
   ▲                   │
Terra (비공개) ─────────┘      ← Terra 는 terra-sdk · terra-agent 를 require
```

규칙: **화살표는 위로만.** Terra와 modules는 서로를 코드로 import하지 않는다(modules의 CI가 pack 검증에 Terra 체크아웃을 쓰는 것은 코드 import가 아니라 도구 호출이고, 이번 단계에서 없앨 수 있는 것은 없앤다).

| 레포 | 공개 | 역할 |
| --- | --- | --- |
| `StellaxiaLab/terra-sdk` (신규) | 공개 | 모듈이 호스트와 대화하는 약속의 Go 구현과 계약 타입 |
| `StellaxiaLab/terra-agent` (신규) | 공개 | 에이전트 루프 라이브러리 (`agentcore`) |
| `StellaxiaLab/modules` | 공개 | 일반 모듈 14개의 소스·빌드·릴리스 |
| `StellaxiaLab/Terra` | 비공개 | 코어. 레지스트리, 6개 잔류 모듈, 스키마 정본 |

## 3. 공개 SDK 표면 — 약 1,550줄

모듈이 호스트에 대해 아는 것은 **환경변수 6 · 헤더 1 · 경로 2**뿐이다(`Terra:docs/contracts/module-host-http-contract.md`). 언어를 가리지 않는다는 근거가 실측으로 있다(stdlib Python 71줄이 실제 활성화를 통과, `Terra:docs/contracts/examples/module-host-minimal.py`). SDK는 Go 모듈이 그 약속을 편하게 지키게 하는 도우미다.

| 조각 | 원본 위치 (Terra `origin/main`) | 줄 수 | 비고 |
| --- | --- | --- | --- |
| 호스팅 | `products/common/packages/terra-module-sdk/{sdk,config,core_client,data_dir,svi_sink}.go` | 약 620 | `Listen/Serve`, `FromEnv`, `CoreClient.Invoke`, `ModuleConfig`, `DataDir`, `SVISink` |
| 계약 타입·상수 | `terra-module-runtime/{identity,handshake,hostadapter,svi_sink}.go` 중 SDK가 쓰는 것, `capability_broker.go`의 `ErrCoreOperationDenied` | 약 300 | `CoreInvocation/Result`, `WorkloadIdentity`, `HandshakeResponse`, `ReadinessResponse`, `SVISink*`, 경로·헤더·환경변수명. 전부 stdlib만 쓰는 구조체·상수 |
| 위임 도어 | `terra-module-runtime/gateway_delegate.go` | 68 | `GatewayDelegate*`, `DelegatedCredentialPrefix`, `TraceIDHeader` |
| 공유 루트 | `terra-module-runtime/shared_roots.go` | 131 | `SharedRoot`, `NameSharedRoots`, `SharedRootsEnv` |
| SVI 타입 | `terra-svi/types.go` | 192 | `ResourceDescriptor` 외 15개. `time`만 import, 연쇄 없음 |
| 입력 타입 | `terra-protocol/{inputevent,inputpermission,virtualinput}.go` | 약 240 | stdlib만. 3파일 자기완결(컴파일 검증은 미실시) |

공개하지 **않는** 것: `terra-module-runtime`의 활성화·감독·호스트 쪽 나머지(약 7,000줄), `terra-protocol`의 나머지(전송·티켓·위임 등), `terra-svi`의 나머지(검증·변환·바인딩 상태).

### 모듈이 실제로 쓰는 코어 심볼 (modules `origin/main` 전수 스캔)

| 모듈 | 패키지 | 심볼 수 | 대표 |
| --- | --- | --- | --- |
| agent | terra-agent-core | 27 | `Agent`, `Approver`, `Autonomy*`, `Tool*`, `Transport`, `Record*` |
| agent | module-runtime | 5 | `CoreInvocation`, `GatewayDelegate*`, `DelegatedCredentialPrefix` |
| io-inventory | terra-svi | 15 | `ResourceDescriptor`, `EndpointDescriptor`, `QoSProfile`, 상태 상수 |
| io-inventory | terra-protocol | 2 | `InputTierForKind`, `IsTerraVirtualInput` |
| io-weave | terra-io-weave | 6 | `Translator`, `Profile`, `NewTranslator` |
| io-weave | terra-protocol | 14 | 포인터 입력 타입, `VirtualInput*` |
| io-weave | terra-svi | 10 | `ResourceDescriptor` 계열 |
| file | module-runtime | 3 | `SharedRoot`, `NameSharedRoots`, `SharedRootsEnv` |
| fleet | module-runtime | 2 | `CoreInvocation`, `ErrCoreOperationDenied` |
| nodetalk | module-runtime | 4 | `CoreInvocation`, `CredentialHeader`, `ReadinessPath`, `WorkloadIdentity` |

## 4. 열린 질문 (세션이 만나면 이 기본 가정으로 진행하고 결과를 보고한다)

| ID | 질문 | 기본 가정 |
| --- | --- | --- |
| P-1 | `agentcore`가 쓰는 `CatalogOperation`(과 딸린 `CatalogAvailability/Execution/Output/SideEffect`)은 현재 `terra-api-contract/catalog.go`(패키지 2,640줄 중 일부)에 있다. 어디에 둘까 | `terra-agent`가 **필요한 필드만 자체 정의**해 외부 의존 0을 유지한다. Terra의 `api-contract`는 변환하거나 구조 호환으로 맞춘다. 딸린 타입의 연쇄는 읽지 않았으니 추출하며 확인한다 |
| P-2 | `terra-sdk`를 단일 Go 모듈로 둘지 | 단일 모듈 하나 |
| P-3 | 라이선스 | **지정하지 않았다.** `modules`에도 LICENSE가 없다. 공개 레포 라이선스는 사용자가 정한다. 세션은 임의로 추가하지 않는다 |
| P-4 | Go 최소 버전 | `terra-module-sdk` 현재 `go 1.23.0`, `io.terra.file`은 `os.Root`를 써서 `go 1.25.0`. SDK는 가장 낮은 값으로 시작 |

## 5. 레포별 작업

작업 ID는 접두사로 레포를 나눈다: **S**=terra-sdk, **A**=terra-agent, **M**=modules, **T**=Terra.
규모: 작음(반나절 이하) · 중(수일) · 큼(주 단위).

### 5.1 terra-sdk

| ID | 작업 | 규모 |
| --- | --- | --- |
| S-1 | 레포 골격: `go.mod`(`github.com/StellaxiaLab/terra-sdk`), README, CI(테스트), 태그 기반 릴리스 | 작음 |
| S-2 | 호스팅 코어 이전: `Listen/Serve`, 핸드셰이크·준비 확인·자격 검증, `FromEnv`, `ModuleConfig/LoadModuleConfig`, `DataDir` | 중 |
| S-3 | 계약 타입·상수·오류 이전 (§3 표 2행) | 중 |
| S-4 | 위임 도어와 공유 루트 이전 (§3 표 3·4행) | 작음 |
| S-5 | SVI 타입 `types.go` 이전 | 작음 |
| S-6 | 입력 타입 3파일 이전 | 작음 |
| S-7 | **적합성 테스트 키트**: Terra의 `products/common/tests/module-gateway-integration`가 하는 계약 검증(특히 `TestTheDocumentedHTTPContractIsEnoughForANonGoModule`)을 Terra 없이 외부에서 돌릴 수 있게 분리 | 중 |
| S-8 | 최소 예제(Python)와 계약 문서 사본, **버전 정책 문서**(semver, v0.x 동안 깨짐 허용 범위, 폐기 기간) | 작음 |

옮기는 코드는 **복사 후 경로·패키지만 바꾼다.** 동작을 고치지 않는다. 테스트도 같이 가져온다.

### 5.2 terra-agent

| ID | 작업 | 규모 |
| --- | --- | --- |
| A-1 | 레포 골격: `go.mod`(`github.com/StellaxiaLab/terra-agent`), README, CI, 태그 릴리스 | 작음 |
| A-2 | `products/common/packages/terra-agent-core` 이전(패키지명 `agentcore`, 1,591줄과 테스트) | 중 |
| A-3 | P-1 처리: `CatalogOperation` 계열의 필요한 필드를 자체 정의, `terra-api-contract` 의존 제거 | 중 |
| A-4 | 공개 API 목록 문서화(CLI와 모듈이 쓰는 심볼 — 모듈 27개 + CLI의 `New/Options/Recorder/Answer/Tool*` 등) | 작음 |

### 5.3 modules

| ID | 작업 | 규모 |
| --- | --- | --- |
| M-1 | Go 모듈 경로 개명 `github.com/terra-project/terra/module/...` → `github.com/StellaxiaLab/...`, 자기 경로 import **49파일** 기계 치환 | 작음 |
| M-2 | `go.work` replace 제거. 10개 Go 모듈이 `terra-sdk`를 버전으로 require. `module-runtime`·`svi`·`protocol` import를 SDK로 교체 | 중 |
| M-3 | `io.terra.agent`: `terra-agent`를 require, `module-runtime` 5개 심볼은 SDK로 | 중 |
| M-4 | `io.terra.io-weave`: `terra-io-weave` 512줄을 모듈 안으로 가져옴(`weave/` 또는 하위 패키지). 그 안의 `terra-protocol` 의존은 SDK로 | 작음 |
| M-5 | CI: `TERRA_CHECKOUT_SSH_KEY` 의존 제거(pack 검증이 Terra 체크아웃을 쓰는 부분이 남으면 이유를 문서에 적고 최소화), 스키마 대조를 건너뛰지 못하게 | 작음 |
| M-6 | `check-schema-drift`: Terra 체크아웃이 없을 때 "건너뜀"이 아니라 정본을 받아 오거나 실패하게 | 작음 |
| M-7 | Terra에서 `examples/scene-login-demo` 이관 수령 | 작음 |
| M-8 | README·`docs/layout.md`에 독립 빌드 절차(클론 한 번으로 빌드) 반영 | 작음 |

**완료 정의**: 빈 머신에서 `git clone modules` 한 번으로 14개 모듈이 빌드되고, CI가 Terra SSH 키 없이 통과한다.

### 5.4 Terra

| ID | 작업 | 규모 |
| --- | --- | --- |
| T-1 | 별칭화: `terra-module-runtime`·`terra-svi`·`terra-protocol`의 SDK 이전 대상 타입을 `type X = sdk.X`로 전환. 코어 `go.mod`가 공개 `terra-sdk`를 require. **호출부 무변경이 목표**이고 전체 `go test -count=1`로 확인한다(코어에서 `svi`를 쓰는 곳 daemon 13·master 4·api 4·cli 2·sdk 14, `protocol`은 master `control_plane` 15·daemon `service_tunnel` 8 등) | 중 |
| T-2 | 내부 `terra-module-sdk` 제거 → 공개 SDK로 대체 | 작음 |
| T-3 | `terra-agent-core` 제거 → `terra-cli`(`internal/mcp/server.go`, `internal/app/mcp.go`, `internal/client/client.go`)가 `terra-agent`를 require | 중 |
| T-4 | `terra-io-weave` 제거(M-4 이후) | 작음 |
| T-5 | `go.work` 정리, `Test-ProductSourceLayout.ps1` 등 경로를 가정하는 도구 확인 | 작음 |
| T-6 | `examples/scene-login-demo`를 `modules`로 이관(M-7과 짝), `module/README.md`와 구현 인벤토리(`docs/reference/implemented-features.md`)·점검표 갱신 | 작음 |
| — | **변경 없음**: tree 레지스트리, 6개 잔류 모듈, 매니페스트 스키마 정본, 번들 구성 | – |

## 6. 순서

| 단계 | 내용 | 병행 |
| --- | --- | --- |
| 1 | S-1, A-1 (골격) | 병행 |
| 2 | S-2~S-6, A-2~A-3 | 병행 |
| 3 | S-7·S-8, A-4, 두 레포 **v0.1.0 태그** | 병행 |
| 4 | T-1·T-2·T-3 (Terra가 소비) | 3단계 후 |
| 5 | M-1~M-4 (modules 전환) | 3단계 후, 4단계와 병행 가능 |
| 6 | M-5~M-8, T-4~T-6 정리 | 마지막 |

세션 간 조율 규칙:

1. 의존 방향 위쪽 레포의 **태그가 선 뒤에** 아래 레포가 그 태그를 쓴다. 태그가 없으면 `replace`를 임시로 쓰되 PR 본문에 "태그 후 제거"를 적는다.
2. 다른 레포의 문서·코드를 **직접 고치지 않는다.** 필요한 변경은 해당 레포 PR 설명이나 이슈로 남긴다.
3. 계약(경로·헤더·환경변수명·타입 필드)을 바꾸고 싶으면 멈추고 사용자에게 묻는다. 이번 작업은 **이동**이지 변경이 아니다.

## 7. 확인하지 못한 것

- `terra-protocol` 3파일이 같은 패키지의 다른 파일 식별자를 참조하는지(정의 위치 검색까지만 했고 컴파일로 검증하지 않음).
- `CatalogOperation` 딸린 타입의 연쇄.
- T-1 별칭 전환이 코어 전체를 깨지 않는지(이론상 안전, 빌드 미확인).
- `agent` 모듈이 `agentcore` 27개 심볼 중 실제로 호출하는 경로가 CLI와 겹치는지.
- `bundled-modules.json`의 위치와 번들 구성이 모듈 경로 개명에 영향받는지.
- 로컬 Terra `main`은 `origin/main`보다 423커밋 뒤처져 있었다. **작업 전 `git fetch`하고 `origin/main`에서 새 브랜치를 딴다.**

## 8. 하지 않는 일 (1단계 범위 밖)

서명 체계(발급·폐기·키 배포), 퍼블리셔 계정·심사, 공개 접근 엔드포인트, 마켓 화면, 코어 버전 범위 필드(`compatibility`), 번들 버전 고정, `configuration` 첫 채택, 레지스트리 코드 변경.

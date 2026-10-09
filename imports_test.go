package agentcore

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agentcore가 무엇을 임포트하지 않는지가 이 패키지의 정의다.
//
// 모델 SDK를 임포트하면 승인 규칙이 provider를 알게 되고, MCP를 임포트하면 모드
// B 전용이 되며, 모듈 런타임을 임포트하면 CLI가 링크할 수 없다. 셋 중 무엇이든
// 들어오는 순간 "두 호스트가 같은 게이트를 쓴다"가 깨지는데, 깨진 것은 컴파일이
// 아니라 설계라서 아무도 알아채지 못한다. 그래서 테스트가 지킨다.
func TestAgentCoreImportsNothingItMustNot(t *testing.T) {
	// 외부 의존 0이다. 허용 목록이 비어 있다는 것이 이 레포의 약속이다.
	allowed := map[string]bool{}
	// 표준 라이브러리라고 다 괜찮은 것은 아니다. net/http를 임포트하는 순간 이
	// 패키지가 스스로 전송을 하게 되고, 그러면 Transport 인터페이스는 장식이
	// 된다. os/exec은 더 나쁘다 — 게이트 뒤에 있어야 할 실행이 게이트 안에서
	// 일어난다.
	forbiddenStandard := map[string]bool{
		"net":      true,
		"net/http": true,
		"os/exec":  true,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fileSet, filepath.Join(".", name), nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, imported := range file.Imports {
			path := strings.Trim(imported.Path.Value, `"`)
			// 표준 라이브러리는 첫 구간에 점이 없다.
			if !strings.Contains(strings.SplitN(path, "/", 2)[0], ".") {
				if forbiddenStandard[path] {
					t.Errorf("%s imports %q — agentcore decides, it does not reach out", name, path)
				}
				continue
			}
			if allowed[path] {
				continue
			}
			t.Errorf("%s imports %q — agentcore must stay free of transport, model and protocol dependencies", name, path)
		}
	}
}

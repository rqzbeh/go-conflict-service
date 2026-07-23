package conflict

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("RUN_REAL_LLM") != "1" {
		_ = os.Unsetenv("OPENAI_BASE_URL")
		_ = os.Unsetenv("OPENAI_API_KEY")
		_ = os.Unsetenv("OPENAI_MODEL")
		_ = os.Unsetenv("OPENAI_TIMEOUT_SECONDS")
	}
	os.Exit(m.Run())
}

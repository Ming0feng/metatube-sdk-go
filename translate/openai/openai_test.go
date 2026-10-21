package openai

import (
	"os"
	"testing"

	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
)

func TestOpenaiTranslate(t *testing.T) {
	// Without an API key the client would fall back to the default
	// `https://api.openai.com`, which is unreachable in many networks
	// and would block the whole test run until the go test timeout.
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY is not set, skipping OpenAI translation test")
	}
	apiURL := os.Getenv("OPENAI_API_URL")

	for _, unit := range []struct {
		text, from, to string
	}{
		{`Oh yeah! I'm a translator!`, "", "zh-CN"},
		{`Oh yeah! I'm a translator!`, "", "zh-TW"},
		{`Oh yeah! I'm a translator!`, "", "ja"},
		{`Oh yeah! I'm a translator!`, "", "de"},
		{`Oh yeah! I'm a translator!`, "", "fr"},
	} {
		result, err := (&OpenAI{
			APIKey: apiKey,
			APIUrl: apiURL,
			Model:  openai.GPT4o,
		}).Translate(unit.text, unit.from, unit.to)
		if assert.NoError(t, err) {
			t.Log(result)
		}
	}
}

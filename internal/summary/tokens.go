package summary

import (
	"sync"

	tiktoken "github.com/hupe1980/go-tiktoken"
)

// encoding is built once and shared by all summarizers; tiktoken encodings are
// immutable after construction, so concurrent Encode calls are safe.
var encoding = sync.OnceValues(func() (*tiktoken.Encoding, error) {
	return tiktoken.NewEncodingForModel("ada")
})

// estimateTokens gives a rough token count for text. The "ada" encoding is not
// the target model's tokenizer, so treat the result as an estimate only.
func estimateTokens(text string) (int, error) {
	enc, err := encoding()
	if err != nil {
		return 0, err
	}

	_, tokens, err := enc.Encode(text, nil, nil)
	if err != nil {
		return 0, err
	}

	return len(tokens), nil
}

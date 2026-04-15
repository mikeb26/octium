/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package llmclient

import (
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/mikeb26/octium/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReasoningModelOption_OpenAI_GPT54_MediumEffort_ElidesExplicitSetting(t *testing.T) {
	client := &EINOAIClient{
		vendor:          "openai",
		model:           "gpt-5.4",
		reasoningEffort: types.ReasoningEffortMedium,
	}

	opt, include, err := client.reasoningModelOption()
	require.NoError(t, err)
	assert.False(t, include)
	assert.Equal(t, opt, opt) // sanity: model.Option is comparable

	// In the gpt-5.4* openai path we should return include=false,
	// indicating we are not setting reasoning effort explicitly.
	assert.Equal(t, model.Option{}, opt)
}

func TestReasoningModelOption_OpenAI_GPT54_NonMediumEffort_Fails(t *testing.T) {
	client := &EINOAIClient{
		vendor:          "openai",
		model:           "gpt-5.4-mini",
		reasoningEffort: types.ReasoningEffortHigh,
	}

	_, _, err := client.reasoningModelOption()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReasoningEffortNotSupported)
}

func TestReasoningModelOption_OpenAI_NonGPT54_AllowsExplicitSetting(t *testing.T) {
	client := &EINOAIClient{
		vendor:          "openai",
		model:           "gpt-5.2",
		reasoningEffort: types.ReasoningEffortHigh,
	}

	opt, include, err := client.reasoningModelOption()
	require.NoError(t, err)
	assert.True(t, include)
	assert.NotEqual(t, model.Option{}, opt)
}

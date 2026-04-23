/* Copyright © 2023-2025 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package internal

const DefaultVendor = "openai"

type VendorInfo struct {
	Name            string
	FullName        string
	ApiKeyUrl       string
	SupportedModels []string
	DefaultModel    string
}

var vendorInfos = map[string]VendorInfo{
	"google": {
		Name:      "google",
		FullName:  "Google",
		ApiKeyUrl: "https://aistudio.google.com/app/api-keys",
		SupportedModels: []string{
			"gemini-2.5-pro",
			"gemini-2.5-flash",
			"gemini-2.5-flash-lite",
			"gemini-3-flash-preview",
			"gemini-3.1-flash-lite-preview",
			"gemini-3.1-pro-preview",
			"gemini-3.1-pro-preview-customtools",
		},
		DefaultModel: "gemini-3.1-pro-preview-customtools",
	},
	"openrouter": {
		Name:      "openrouter",
		FullName:  "OpenRouter",
		ApiKeyUrl: "https://openrouter.ai/keys",
		SupportedModels: []string{
			"anthropic/claude-haiku-4.5",
			"anthropic/claude-sonnet-4.5",
			"anthropic/claude-sonnet-4.6",
			"anthropic/claude-opus-4.6",
			"anthropic/claude-opus-4.7",
			"deepseek/deepseek-r1",
			"deepseek/deepseek-v3.2",
			"google/gemini-2.5-flash",
			"google/gemini-2.5-flash-lite",
			"google/gemini-2.5-pro",
			"google/gemini-3-flash-preview",
			"google/gemini-3-pro-preview",
			"google/gemini-3.1-flash-lite-preview",
			"google/gemini-3.1-pro-preview",
			"google/gemini-3.1-pro-preview-customtools",
			"meta-llama/llama-3.3-70b-instruct",
			"mistralai/codestral-2508",
			"mistralai/mistral-large",
			"mistralai/mistral-medium-3.1",
			"mistralai/mistral-small-3.2-24b-instruct",
			"moonshotai/kimi-k2-thinking",
			"moonshotai/kimi-k2.5",
			"openai/gpt-5",
			"openai/gpt-5-mini",
			"openai/gpt-5-nano",
			"openai/gpt-5.1",
			"openai/gpt-5.2",
			"openai/gpt-5.2-pro",
			"openai/gpt-oss-120b",
			"openai/gpt-5.3-codex",
			"openai/gpt-5.3-chat",
			"openai/gpt-5.4",
			"openai/gpt-5.4-mini",
			"openai/gpt-5.4-nano",
			"openai/gpt-5.4-pro",
			"perplexity/sonar",
			"perplexity/sonar-pro",
			"qwen/qwen3-235b-a22b",
			"x-ai/grok-3",
			"x-ai/grok-3-mini",
			"x-ai/grok-4",
		},
		DefaultModel: "openai/gpt-5.4",
	},
	"anthropic": {
		Name:      "anthropic",
		FullName:  "Anthropic",
		ApiKeyUrl: "https://platform.claude.com/settings/keys",
		SupportedModels: []string{
			"claude-sonnet-4-5-20250929",
			"claude-opus-4-5-20251101",
			"claude-haiku-4-5-20251001",
			"claude-sonnet-4-6",
			"claude-opus-4-6",
			"claude-opus-4-7",
		},
		DefaultModel: "claude-sonnet-4-6",
	},
	"openai": {
		Name:      "openai",
		FullName:  "OpenAI",
		ApiKeyUrl: "https://platform.openai.com/api-keys",
		SupportedModels: []string{
			"gpt-5-mini",
			"gpt-5.2",
			"gpt-5.2-pro",
			"gpt-5.4",
			"gpt-5.4-mini",
			"gpt-5.4-nano",
			"gpt-5.4-pro",
		},
		DefaultModel: "gpt-5.4",
	},
}

func GetVendors() []string {
	ret := make([]string, 0)

	for k, _ := range vendorInfos {
		ret = append(ret, k)
	}

	return ret
}

func GetVendorInfo(name string) VendorInfo {
	v, ok := vendorInfos[name]
	if !ok {
		return VendorInfo{}
	}

	return v
}

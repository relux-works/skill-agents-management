package anthropic

import (
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin"
	"github.com/relux-works/skill-agents-management/pkg/vendorplugin/benchdata"
)

// Model facts are declared once in benchdata; this adapter adds runtime types.
var models = vendorplugin.ModelsFromBenchdata(benchdata.AnthropicModels())

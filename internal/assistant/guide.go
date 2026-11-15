package assistant

import _ "embed"

const GuideVersion = "business-agent-v1.3"

//go:embed guides/agent.md
var productGuide string

func SystemInstruction() string { return instruction + "\n\n" + productGuide }

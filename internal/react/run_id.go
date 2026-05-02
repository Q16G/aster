package react

// GenerateAgentRunID returns a new internal run id for an agent execution.
// It is only used for in-memory / persistence linkage and is not part of the
// session shared workspace or frontend aggregation truth.
func GenerateAgentRunID() string {
	return generateAgentRunID()
}

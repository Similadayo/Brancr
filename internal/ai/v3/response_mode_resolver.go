package v3

import "github.com/similadayo/brancr/internal/conversation"

func ResolveResponseMode(goal conversation.Goal) ResponseMode {
	switch goal {
	case conversation.GoalExplore:
		return ModeGentle
	case conversation.GoalDecide:
		return ModeBalanced
	case conversation.GoalCommit, conversation.GoalResolve:
		return ModeConfident
	default:
		return ModeBalanced
	}
}

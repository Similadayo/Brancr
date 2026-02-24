package utils

func IsOwner(telegramID string, ownerHandle string) bool {
	return telegramID == ownerHandle
}

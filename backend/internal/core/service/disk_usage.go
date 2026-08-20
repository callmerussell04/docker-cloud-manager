package service

func bytesToMBRoundedUp(bytes int64) int64 {
	if bytes <= 0 {
		return 0
	}
	const mb = 1024 * 1024
	return (bytes + mb - 1) / mb
}

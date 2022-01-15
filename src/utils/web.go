package utils

import (
	"fmt"
)

func GetCdnURL(url string, emoteID string, size int8) string {
	return fmt.Sprintf("%v/emote/%v/%dx", url, emoteID, size)
}

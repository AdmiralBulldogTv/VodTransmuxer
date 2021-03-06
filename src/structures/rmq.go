package structures

import "go.mongodb.org/mongo-driver/bson/primitive"

type VodTranscodeJob struct {
	VodID   primitive.ObjectID `json:"vod_id"`
	Variant VodVariant         `json:"variant"`
}

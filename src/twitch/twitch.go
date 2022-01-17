package twitch

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/AdmiralBulldogTv/VodTransmuxer/src/global"
	"github.com/go-redis/redis/v8"
	jsoniter "github.com/json-iterator/go"
	"github.com/nicklaw5/helix"
	"github.com/sirupsen/logrus"
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary

func GetAuth(gCtx global.Context, ctx context.Context) (string, error) {
	token, err := gCtx.Inst().Redis.Get(ctx, "twitch:app-token")
	if err == nil {
		return token.(string), nil
	}

	if err != redis.Nil {
		logrus.Error("failed to query redis: ", err)
	}

	v := url.Values{}

	v.Set("client_id", gCtx.Config().Twitch.ClientID)
	v.Set("client_secret", gCtx.Config().Twitch.ClientSecret)
	v.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, "POST", "https://id.twitch.tv/oauth2/token", strings.NewReader(v.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Content-Length", strconv.Itoa(len(v.Encode())))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	tokenResp := helix.AccessCredentials{}
	if err := json.Unmarshal(data, &tokenResp); err != nil {
		return "", err
	}

	if err := gCtx.Inst().Redis.SetEX(ctx, "twitch:app-token", tokenResp.AccessToken, time.Second*time.Duration(tokenResp.ExpiresIn)); err != nil {
		logrus.Error("failed to store token in redis: ", err)
	}

	return tokenResp.AccessToken, nil
}

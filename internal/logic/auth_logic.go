package logic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"learn_future/internal/svc"
	"learn_future/internal/tutorial"
	"learn_future/internal/types"
	"learn_future/pkg/jwt"
)

type AuthLogic struct {
	svcCtx *svc.ServiceContext
}

func NewAuthLogic(svcCtx *svc.ServiceContext) *AuthLogic {
	return &AuthLogic{svcCtx: svcCtx}
}

type wxSessionResp struct {
	OpenID     string `json:"openid"`
	SessionKey string `json:"session_key"`
	ErrCode    int    `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
}

func (l *AuthLogic) WxLogin(req *types.WxLoginReq) (*types.WxLoginResp, *types.TutorialCard, error) {
	if req.Code == "" {
		return nil, nil, errors.New("code is required")
	}

	// Exchange code for openid via WeChat API
	openID, err := l.getOpenID(req.Code)
	if err != nil {
		return nil, nil, err
	}

	// Find or create user
	user, err := l.svcCtx.UserModel.FindByOpenID(openID)
	if err != nil {
		return nil, nil, err
	}

	isNewUser := false
	if user == nil {
		isNewUser = true
		user, err = l.svcCtx.UserModel.Create(openID, "", "")
		if err != nil {
			return nil, nil, err
		}

		// Create account with initial balance
		initialBalance, _ := decimal.NewFromString(l.svcCtx.Config.Trading.InitialBalance)
		_, err = l.svcCtx.AccountModel.Create(user.ID, initialBalance)
		if err != nil {
			return nil, nil, err
		}
		// Load into memory account cache for matching engine
		l.svcCtx.TradingEngine.GetMemAccounts().Load(user.ID, initialBalance, decimal.Zero)
	}

	// Generate tokens
	token, err := jwt.GenerateToken(user.ID, l.svcCtx.Config.JWT.Secret, l.svcCtx.Config.JWT.Expire)
	if err != nil {
		return nil, nil, err
	}
	refreshToken, err := jwt.GenerateToken(user.ID, l.svcCtx.Config.JWT.Secret, l.svcCtx.Config.JWT.RefreshExpire)
	if err != nil {
		return nil, nil, err
	}

	// Tutorial for new users
	var card *types.TutorialCard
	if isNewUser {
		card = tutorial.Topics[tutorial.TopicPerpetualIntro]
	}

	return &types.WxLoginResp{
		Token:        token,
		RefreshToken: refreshToken,
		UserID:       user.ID,
	}, card, nil
}

func (l *AuthLogic) RefreshToken(req *types.RefreshTokenReq) (*types.RefreshTokenResp, error) {
	claims, err := jwt.ParseToken(req.RefreshToken, l.svcCtx.Config.JWT.Secret)
	if err != nil {
		return nil, errors.New("invalid refresh token")
	}

	token, err := jwt.GenerateToken(claims.UserID, l.svcCtx.Config.JWT.Secret, l.svcCtx.Config.JWT.Expire)
	if err != nil {
		return nil, err
	}
	refreshToken, err := jwt.GenerateToken(claims.UserID, l.svcCtx.Config.JWT.Secret, l.svcCtx.Config.JWT.RefreshExpire)
	if err != nil {
		return nil, err
	}

	return &types.RefreshTokenResp{
		Token:        token,
		RefreshToken: refreshToken,
	}, nil
}

func (l *AuthLogic) getOpenID(code string) (string, error) {
	cfg := l.svcCtx.Config.WeChat
	url := fmt.Sprintf(
		"https://api.weixin.qq.com/sns/jscode2session?appid=%s&secret=%s&js_code=%s&grant_type=authorization_code",
		cfg.AppID, cfg.AppSecret, code,
	)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("wechat api error: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result wxSessionResp
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("wechat error: %d %s", result.ErrCode, result.ErrMsg)
	}
	if result.OpenID == "" {
		return "", errors.New("empty openid from wechat")
	}

	return result.OpenID, nil
}

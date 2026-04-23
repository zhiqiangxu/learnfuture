package handler

import (
	"net/http"

	"github.com/shopspring/decimal"

	"learn_future/internal/middleware"
	"learn_future/internal/svc"
	"learn_future/internal/types"
	"learn_future/pkg/response"
)

func LeaderboardPnlHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		entries, err := svcCtx.LeaderboardModel.GetPnlRanking(50)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}

		list := make([]types.LeaderboardEntry, 0, len(entries))
		for _, e := range entries {
			list = append(list, types.LeaderboardEntry{
				Rank:     e.Rank,
				UserID:   e.UserID,
				Nickname: e.Nickname,
				Value:    e.Value.StringFixed(2),
			})
		}

		var my *types.LeaderboardEntry
		myEntry, _ := svcCtx.LeaderboardModel.GetUserPnlRank(userID)
		if myEntry != nil {
			my = &types.LeaderboardEntry{
				Rank:     myEntry.Rank,
				UserID:   myEntry.UserID,
				Nickname: myEntry.Nickname,
				Value:    myEntry.Value.StringFixed(2),
			}
		}

		response.Success(w, &types.LeaderboardResp{List: list, My: my})
	}
}

func LeaderboardROIHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		initialBalance, _ := decimal.NewFromString(svcCtx.Config.Trading.InitialBalance)

		entries, err := svcCtx.LeaderboardModel.GetROIRanking(50, initialBalance)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}

		list := make([]types.LeaderboardEntry, 0, len(entries))
		for _, e := range entries {
			list = append(list, types.LeaderboardEntry{
				Rank:     e.Rank,
				UserID:   e.UserID,
				Nickname: e.Nickname,
				Value:    e.Value.StringFixed(2) + "%",
			})
		}

		response.Success(w, &types.LeaderboardResp{List: list})
	}
}

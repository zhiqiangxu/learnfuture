package handler

import (
	"encoding/json"
	"net/http"

	"learn_future/internal/middleware"
	"learn_future/internal/svc"
	"learn_future/internal/tutorial"
	"learn_future/internal/types"
	"learn_future/pkg/response"
)

func TutorialProgressHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.GetUserID(r.Context())

		completed, err := svcCtx.TutorialModel.GetCompleted(userID)
		if err != nil {
			response.Error(w, 500, err.Error())
			return
		}

		topicIDs := make([]string, 0)
		for id := range completed {
			topicIDs = append(topicIDs, id)
		}

		response.Success(w, &types.TutorialStatus{
			Completed: len(completed),
			Total:     tutorial.TotalTopics,
			Topics:    topicIDs,
		})
	}
}

func CompleteTutorialHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.CompleteTutorialReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, 400, "invalid request")
			return
		}

		userID := middleware.GetUserID(r.Context())

		if _, ok := tutorial.GetTopics("")[req.TopicID]; !ok {
			response.Error(w, 400, "invalid topic_id")
			return
		}

		if err := svcCtx.TutorialModel.MarkComplete(userID, req.TopicID); err != nil {
			response.Error(w, 500, err.Error())
			return
		}

		response.Success(w, nil)
	}
}

package logic

import (
	"errors"

	"github.com/shopspring/decimal"

	"learn_future/internal/svc"
	"learn_future/internal/tutorial"
	"learn_future/internal/types"
)

type AccountLogic struct {
	svcCtx *svc.ServiceContext
}

func NewAccountLogic(svcCtx *svc.ServiceContext) *AccountLogic {
	return &AccountLogic{svcCtx: svcCtx}
}

func (l *AccountLogic) GetInfo(userID int64) (*types.AccountInfoResp, error) {
	account, err := l.svcCtx.AccountModel.FindByUserID(userID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, errors.New("account not found")
	}

	available := account.Balance.Sub(account.Frozen)

	// Get tutorial progress
	completedCount, _ := l.svcCtx.TutorialModel.GetCompletedCount(userID)

	return &types.AccountInfoResp{
		Balance:   account.Balance.StringFixed(2),
		Frozen:    account.Frozen.StringFixed(2),
		TotalPnl:  account.TotalPnl.StringFixed(2),
		Available: available.StringFixed(2),
		Progress: &types.TutorialStatus{
			Completed: completedCount,
			Total:     tutorial.TotalTopics,
		},
	}, nil
}

func (l *AccountLogic) Reset(userID int64) (*types.ResetAccountResp, error) {
	// Check no active positions
	positions := l.svcCtx.PositionCache.GetByUser(userID)
	if len(positions) > 0 {
		return nil, errors.New("please close all positions before resetting")
	}

	// Check no pending orders
	orders := l.svcCtx.OrderCache.GetByUser(userID)
	if len(orders) > 0 {
		return nil, errors.New("please cancel all pending orders before resetting")
	}

	initialBalance, _ := decimal.NewFromString(l.svcCtx.Config.Trading.InitialBalance)
	if err := l.svcCtx.AccountModel.Reset(userID, initialBalance); err != nil {
		return nil, err
	}

	// Sync in-memory account balance (source of truth for matching engine)
	l.svcCtx.TradingEngine.GetMemAccounts().Reset(userID, initialBalance)

	return &types.ResetAccountResp{
		Balance: initialBalance.StringFixed(2),
	}, nil
}

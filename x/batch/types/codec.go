package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the x/batch concrete types on the amino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgSubmitIntent{}, "terra/batch/MsgSubmitIntent", nil)
	cdc.RegisterConcrete(&MsgCancelIntent{}, "terra/batch/MsgCancelIntent", nil)
	cdc.RegisterConcrete(&MsgRegisterSolver{}, "terra/batch/MsgRegisterSolver", nil)
	cdc.RegisterConcrete(&MsgUnbondSolver{}, "terra/batch/MsgUnbondSolver", nil)
	cdc.RegisterConcrete(&MsgDepositSolverEscrow{}, "terra/batch/MsgDepositSolverEscrow", nil)
	cdc.RegisterConcrete(&MsgWithdrawSolverEscrow{}, "terra/batch/MsgWithdrawSolverEscrow", nil)
	cdc.RegisterConcrete(&MsgCommitBid{}, "terra/batch/MsgCommitBid", nil)
	cdc.RegisterConcrete(&MsgRevealBid{}, "terra/batch/MsgRevealBid", nil)
	cdc.RegisterConcrete(&MsgRegisterFrontend{}, "terra/batch/MsgRegisterFrontend", nil)
	cdc.RegisterConcrete(&MsgApproveFrontend{}, "terra/batch/MsgApproveFrontend", nil)
	cdc.RegisterConcrete(&MsgRevokeFrontend{}, "terra/batch/MsgRevokeFrontend", nil)
	cdc.RegisterConcrete(&MsgCreateMarket{}, "terra/batch/MsgCreateMarket", nil)
	cdc.RegisterConcrete(&MsgSetMarketEnabled{}, "terra/batch/MsgSetMarketEnabled", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "terra/batch/MsgUpdateParams", nil)
}

// RegisterInterfaces registers the x/batch message implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgSubmitIntent{},
		&MsgCancelIntent{},
		&MsgRegisterSolver{},
		&MsgUnbondSolver{},
		&MsgDepositSolverEscrow{},
		&MsgWithdrawSolverEscrow{},
		&MsgCommitBid{},
		&MsgRevealBid{},
		&MsgRegisterFrontend{},
		&MsgApproveFrontend{},
		&MsgRevokeFrontend{},
		&MsgCreateMarket{},
		&MsgSetMarketEnabled{},
		&MsgUpdateParams{},
	)
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}

var (
	amino     = codec.NewLegacyAmino()
	ModuleCdc = codec.NewAminoCodec(amino)
)

func init() {
	RegisterLegacyAminoCodec(amino)
	sdk.RegisterLegacyAminoCodec(amino)
	amino.Seal()
}

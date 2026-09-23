package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the x/ismbond concrete types on the amino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgBondOperator{}, "terra/ismbond/MsgBondOperator", nil)
	cdc.RegisterConcrete(&MsgUnbondOperator{}, "terra/ismbond/MsgUnbondOperator", nil)
	cdc.RegisterConcrete(&MsgClaimBond{}, "terra/ismbond/MsgClaimBond", nil)
	cdc.RegisterConcrete(&MsgSubmitEvidence{}, "terra/ismbond/MsgSubmitEvidence", nil)
	cdc.RegisterConcrete(&MsgDistributeRewards{}, "terra/ismbond/MsgDistributeRewards", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "terra/ismbond/MsgUpdateParams", nil)
}

// RegisterInterfaces registers the x/ismbond message implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgBondOperator{},
		&MsgUnbondOperator{},
		&MsgClaimBond{},
		&MsgSubmitEvidence{},
		&MsgDistributeRewards{},
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

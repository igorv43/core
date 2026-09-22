package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the x/liquidstake concrete types on the
// provided LegacyAmino codec (Amino JSON signing for Station and Ledger).
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgStake{}, "terra/liquidstake/MsgStake", nil)
	cdc.RegisterConcrete(&MsgUnstake{}, "terra/liquidstake/MsgUnstake", nil)
	cdc.RegisterConcrete(&MsgClaim{}, "terra/liquidstake/MsgClaim", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "terra/liquidstake/MsgUpdateParams", nil)
}

// RegisterInterfaces registers the x/liquidstake message implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgStake{},
		&MsgUnstake{},
		&MsgClaim{},
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

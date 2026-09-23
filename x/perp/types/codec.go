package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the x/perp concrete types on the amino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgDepositCollateral{}, "terra/perp/MsgDepositCollateral", nil)
	cdc.RegisterConcrete(&MsgWithdrawCollateral{}, "terra/perp/MsgWithdrawCollateral", nil)
	cdc.RegisterConcrete(&MsgSubmitPerpIntent{}, "terra/perp/MsgSubmitPerpIntent", nil)
	cdc.RegisterConcrete(&MsgSubmitTriggerOrder{}, "terra/perp/MsgSubmitTriggerOrder", nil)
	cdc.RegisterConcrete(&MsgCancelTriggerOrder{}, "terra/perp/MsgCancelTriggerOrder", nil)
	cdc.RegisterConcrete(&MsgSetAutoTopUp{}, "terra/perp/MsgSetAutoTopUp", nil)
	cdc.RegisterConcrete(&MsgFundInsurance{}, "terra/perp/MsgFundInsurance", nil)
	cdc.RegisterConcrete(&MsgCreateMarket{}, "terra/perp/MsgCreateMarket", nil)
	cdc.RegisterConcrete(&MsgUpdateMarket{}, "terra/perp/MsgUpdateMarket", nil)
	cdc.RegisterConcrete(&MsgEnableMarket{}, "terra/perp/MsgEnableMarket", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "terra/perp/MsgUpdateParams", nil)
}

// RegisterInterfaces registers the x/perp message implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgDepositCollateral{},
		&MsgWithdrawCollateral{},
		&MsgSubmitPerpIntent{},
		&MsgSubmitTriggerOrder{},
		&MsgCancelTriggerOrder{},
		&MsgSetAutoTopUp{},
		&MsgFundInsurance{},
		&MsgCreateMarket{},
		&MsgUpdateMarket{},
		&MsgEnableMarket{},
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

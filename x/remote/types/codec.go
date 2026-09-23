package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the x/remote concrete types on the amino codec.
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgGrantSessionKey{}, "terra/remote/MsgGrantSessionKey", nil)
	cdc.RegisterConcrete(&MsgRevokeSessionKey{}, "terra/remote/MsgRevokeSessionKey", nil)
	cdc.RegisterConcrete(&MsgRevokeOwnSessionKey{}, "terra/remote/MsgRevokeOwnSessionKey", nil)
	cdc.RegisterConcrete(&MsgWithdraw{}, "terra/remote/MsgWithdraw", nil)
	cdc.RegisterConcrete(&MsgFundPaymaster{}, "terra/remote/MsgFundPaymaster", nil)
	cdc.RegisterConcrete(&MsgCreateRemoteApp{}, "terra/remote/MsgCreateRemoteApp", nil)
	cdc.RegisterConcrete(&MsgSetGateway{}, "terra/remote/MsgSetGateway", nil)
	cdc.RegisterConcrete(&MsgSetBeacon{}, "terra/remote/MsgSetBeacon", nil)
	cdc.RegisterConcrete(&MsgSetExecutor{}, "terra/remote/MsgSetExecutor", nil)
	cdc.RegisterConcrete(&MsgExecutorControl{}, "terra/remote/MsgExecutorControl", nil)
	cdc.RegisterConcrete(&MsgSetPort{}, "terra/remote/MsgSetPort", nil)
	cdc.RegisterConcrete(&MsgUpdateParams{}, "terra/remote/MsgUpdateParams", nil)
}

// RegisterInterfaces registers the x/remote message implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgGrantSessionKey{},
		&MsgRevokeSessionKey{},
		&MsgRevokeOwnSessionKey{},
		&MsgWithdraw{},
		&MsgFundPaymaster{},
		&MsgCreateRemoteApp{},
		&MsgSetGateway{},
		&MsgSetBeacon{},
		&MsgSetExecutor{},
		&MsgExecutorControl{},
		&MsgSetPort{},
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

package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterLegacyAminoCodec registers the x/warpledger concrete types on the
// provided LegacyAmino codec (Amino JSON signing for Station and Ledger).
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgUpdateParams{}, "terra/warpledger/MsgUpdateParams", nil)
	cdc.RegisterConcrete(&MsgSetDomainCap{}, "terra/warpledger/MsgSetDomainCap", nil)
	cdc.RegisterConcrete(&MsgSweepMigration{}, "terra/warpledger/MsgSweepMigration", nil)
}

// RegisterInterfaces registers the x/warpledger message implementations.
func RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgUpdateParams{},
		&MsgSetDomainCap{},
		&MsgSweepMigration{},
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

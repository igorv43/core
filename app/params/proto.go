package params

import (
	"sync"

	"cosmossdk.io/x/tx/signing"
	"github.com/cosmos/cosmos-sdk/codec"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
)

var (
	protoFilesOnce sync.Once
	protoFiles     *protoregistry.Files
)

// mergedProtoFiles returns the file descriptors of every registered proto
// file, rebuilt together. gogoproto registers each generated file at init
// with `AllowUnresolvable`, and Go runs the `.pb.go` inits in file-name
// order: a `query.pb.go` registered before the module's data file (e.g.
// `remote.pb.go`, `warpledger.pb.go`) keeps placeholder descriptors for the
// messages it imports from it, so the AutoCLI (which decodes responses
// through this resolver) rendered those records as `{}`. The merged registry
// has no placeholders; it is what the SDK's simapp validates against.
func mergedProtoFiles() *protoregistry.Files {
	protoFilesOnce.Do(func() {
		files, err := proto.MergedRegistry()
		if err != nil {
			panic(err)
		}
		protoFiles = files
	})
	return protoFiles
}

// MakeEncodingConfig creates an EncodingConfig for an amino based test configuration.
func MakeEncodingConfig() EncodingConfig {
	amino := codec.NewLegacyAmino()
	interfaceRegistry, err := types.NewInterfaceRegistryWithOptions(types.InterfaceRegistryOptions{
		ProtoFiles: mergedProtoFiles(),
		SigningOptions: signing.Options{
			AddressCodec:          addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
			ValidatorAddressCodec: addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32ValidatorAddrPrefix()),
		},
	})
	if err != nil {
		panic(err)
	}
	marshaler := codec.NewProtoCodec(interfaceRegistry)
	txCfg := tx.NewTxConfig(marshaler, tx.DefaultSignModes)

	return EncodingConfig{
		InterfaceRegistry: interfaceRegistry,
		Marshaler:         marshaler,
		TxConfig:          txCfg,
		Amino:             amino,
	}
}

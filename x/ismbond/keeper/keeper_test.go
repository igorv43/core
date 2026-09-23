package keeper_test

import (
	"crypto/ecdsa"
	"strings"
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/keeper"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	pdkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/keeper"
	pdtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/02_post_dispatch/types"
	corekeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/keeper"
	coretypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	terraapp "github.com/classic-terra/core/v4/app"
	apptesting "github.com/classic-terra/core/v4/app/testing"
	"github.com/classic-terra/core/v4/x/ismbond/keeper"
	"github.com/classic-terra/core/v4/x/ismbond/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const (
	chainID     = "ismbond-test"
	localDomain = 1325
	remoteDom   = 97
)

type fixture struct {
	app      *terraapp.TerraApp
	ctx      sdk.Context
	k        keeper.Keeper
	owner    sdk.AccAddress
	operator sdk.AccAddress
	key      *ecdsa.PrivateKey
	valAddr  string
	mailbox  util.HexAddress
	hook     util.HexAddress
	ism      util.HexAddress
}

// setup boots the app and creates a mailbox whose required hook is a merkle
// tree hook and whose default ISM is a 1-of-1 message-id multisig of a fresh
// secp256k1 validator key.
func setup(t *testing.T) *fixture {
	t.Helper()
	h := &apptesting.KeeperTestHelper{}
	h.SetT(t)
	h.Setup(t, chainID)
	app, ctx := h.App, h.Ctx
	require.NoError(t, app.IsmBondKeeper.InitGenesis(ctx, types.DefaultGenesisState()))
	f := &fixture{app: app, ctx: ctx, k: app.IsmBondKeeper}
	f.owner = sdk.AccAddress([]byte("ismbond-owner--------"))
	f.operator = sdk.AccAddress([]byte("ismbond-operator-----"))
	h.FundAcc(f.owner, sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(1_000_000_000_000))))
	h.FundAcc(f.operator, sdk.NewCoins(sdk.NewCoin("uluna", math.NewInt(5_000_000_000_000_000))))

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	f.key = key
	f.valAddr = strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())

	ismSrv := ismkeeper.NewMsgServerImpl(&app.HyperlaneKeeper.IsmKeeper)
	ismRes, err := ismSrv.CreateMessageIdMultisigIsm(ctx, &ismtypes.MsgCreateMessageIdMultisigIsm{Creator: f.owner.String(), Validators: []string{f.valAddr}, Threshold: 1})
	require.NoError(t, err)
	f.ism = ismRes.Id
	pdSrv := pdkeeper.NewMsgServerImpl(&app.HyperlaneKeeper.PostDispatchKeeper)
	noop, err := pdSrv.CreateNoopHook(ctx, &pdtypes.MsgCreateNoopHook{Owner: f.owner.String()})
	require.NoError(t, err)
	coreSrv := corekeeper.NewMsgServerImpl(app.HyperlaneKeeper)
	mb, err := coreSrv.CreateMailbox(ctx, &coretypes.MsgCreateMailbox{Owner: f.owner.String(), LocalDomain: localDomain, DefaultIsm: f.ism, DefaultHook: &noop.Id, RequiredHook: &noop.Id})
	require.NoError(t, err)
	f.mailbox = mb.Id
	hook, err := pdSrv.CreateMerkleTreeHook(ctx, &pdtypes.MsgCreateMerkleTreeHook{Owner: f.owner.String(), MailboxId: f.mailbox})
	require.NoError(t, err)
	f.hook = hook.Id
	_, err = coreSrv.SetMailbox(ctx, &coretypes.MsgSetMailbox{Owner: f.owner.String(), MailboxId: f.mailbox, RequiredHook: &hook.Id, DefaultHook: &noop.Id})
	require.NoError(t, err)
	return f
}

func (f *fixture) sign(t *testing.T, cp types.Checkpoint) types.Checkpoint {
	t.Helper()
	digest, err := cp.Digest()
	require.NoError(t, err)
	sig, err := crypto.Sign(digest[:], f.key)
	require.NoError(t, err)
	sig[64] += 27
	cp.Signature = sig
	return cp
}

func (f *fixture) bond(t *testing.T) {
	t.Helper()
	require.NoError(t, f.k.Bond(f.ctx, f.operator.String(), f.valAddr, sdk.NewCoin("uluna", math.NewInt(1_000_000_000_000))))
}

func root(b byte) []byte {
	r := make([]byte, 32)
	r[0] = b
	return r
}

func TestBondUnbondClaimAndIsmBonded(t *testing.T) {
	f := setup(t)
	res, err := f.k.IsBonded(f.ctx, f.ism)
	require.NoError(t, err)
	require.False(t, res.Bonded)

	f.bond(t)
	res, err = f.k.IsBonded(f.ctx, f.ism)
	require.NoError(t, err)
	require.True(t, res.Bonded)
	require.Equal(t, []string{f.valAddr}, res.BondedValidators)
	bonded, err := f.k.IsmBondedForToken(f.ctx, nil, f.mailbox) // default ISM of the mailbox
	require.NoError(t, err)
	require.True(t, bonded)

	// below the minimum and duplicated bonds are refused
	err = f.k.Bond(f.ctx, f.owner.String(), "0x00000000000000000000000000000000000000aa", sdk.NewCoin("uluna", math.NewInt(1)))
	require.ErrorIs(t, err, types.ErrInsufficientBond)
	err = f.k.Bond(f.ctx, f.owner.String(), f.valAddr, sdk.NewCoin("uluna", math.NewInt(1_000_000_000_000)))
	require.ErrorIs(t, err, types.ErrOperatorExists)

	claimHeight, err := f.k.Unbond(f.ctx, f.operator.String())
	require.NoError(t, err)
	res, _ = f.k.IsBonded(f.ctx, f.ism)
	require.False(t, res.Bonded, "an unbonding operator no longer counts")
	require.ErrorIs(t, f.k.Claim(f.ctx, f.operator.String()), types.ErrNotClaimable)
	f.ctx = f.ctx.WithBlockHeight(claimHeight)
	before := f.app.BankKeeper.GetBalance(f.ctx, f.operator, "uluna").Amount
	require.NoError(t, f.k.Claim(f.ctx, f.operator.String()))
	after := f.app.BankKeeper.GetBalance(f.ctx, f.operator, "uluna").Amount
	require.Equal(t, before.AddRaw(1_000_000_000_000).String(), after.String())
	_, err = f.k.GetOperator(f.ctx, f.operator.String())
	require.ErrorIs(t, err, types.ErrOperatorNotFound)
}

func TestEquivocationSlashes(t *testing.T) {
	f := setup(t)
	f.bond(t)
	base := types.Checkpoint{Origin: localDomain, MerkleTreeHook: f.hook, Index: 7, MessageId: root(9)}
	a := base
	a.Root = root(1)
	b := base
	b.Root = root(2)
	a, b = f.sign(t, a), f.sign(t, b)

	submitter := sdk.AccAddress([]byte("ismbond-submitter----"))
	validator, slashed, reward, err := f.k.SubmitEvidence(f.ctx, submitter.String(), types.EVIDENCE_KIND_EQUIVOCATION, a, b)
	require.NoError(t, err)
	require.Equal(t, f.valAddr, validator)
	require.Equal(t, "1000000000000", slashed.String())
	require.Equal(t, "200000000000", reward.String())
	require.Equal(t, "200000000000", f.app.BankKeeper.GetBalance(f.ctx, submitter, "uluna").Amount.String())
	_, err = f.k.GetOperator(f.ctx, f.operator.String())
	require.ErrorIs(t, err, types.ErrOperatorNotFound)

	// the same evidence again: the validator has no bond left
	_, _, _, err = f.k.SubmitEvidence(f.ctx, submitter.String(), types.EVIDENCE_KIND_EQUIVOCATION, a, b)
	require.ErrorIs(t, err, types.ErrValidatorNotBonded)
}

func TestEquivocationRejectsInvalidEvidence(t *testing.T) {
	f := setup(t)
	f.bond(t)
	a := f.sign(t, types.Checkpoint{Origin: localDomain, MerkleTreeHook: f.hook, Index: 1, Root: root(1), MessageId: root(9)})
	same := f.sign(t, types.Checkpoint{Origin: localDomain, MerkleTreeHook: f.hook, Index: 1, Root: root(1), MessageId: root(9)})
	_, _, _, err := f.k.SubmitEvidence(f.ctx, f.owner.String(), types.EVIDENCE_KIND_EQUIVOCATION, a, same)
	require.ErrorIs(t, err, types.ErrInvalidEvidence, "same root is not equivocation")
	other := f.sign(t, types.Checkpoint{Origin: localDomain, MerkleTreeHook: f.hook, Index: 2, Root: root(2), MessageId: root(9)})
	_, _, _, err = f.k.SubmitEvidence(f.ctx, f.owner.String(), types.EVIDENCE_KIND_EQUIVOCATION, a, other)
	require.ErrorIs(t, err, types.ErrInvalidEvidence, "different index is not equivocation")
	forged := a
	forged.Signature = append([]byte{}, a.Signature...)
	forged.Signature[10] ^= 0xff
	_, _, _, err = f.k.SubmitEvidence(f.ctx, f.owner.String(), types.EVIDENCE_KIND_EQUIVOCATION, forged, other)
	require.Error(t, err)
	// still bonded
	o, err := f.k.GetOperator(f.ctx, f.operator.String())
	require.NoError(t, err)
	require.True(t, o.Bond.IsPositive())
}

func TestRootHistoryAndInvalidOutbound(t *testing.T) {
	f := setup(t)
	f.bond(t)
	// no outbound message yet: nothing recorded, evidence cannot be judged
	cp := f.sign(t, types.Checkpoint{Origin: localDomain, MerkleTreeHook: f.hook, Index: 0, Root: root(5), MessageId: root(9)})
	_, _, _, err := f.k.SubmitEvidence(f.ctx, f.owner.String(), types.EVIDENCE_KIND_INVALID_OUTBOUND, cp, types.Checkpoint{})
	require.ErrorIs(t, err, types.ErrRootNotRecorded)

	// a warp transfer through the wrapped server records the root at index 0
	warpSrv := warpkeeper.NewMsgServerImpl(f.app.WarpKeeper)
	tok, err := warpSrv.CreateCollateralToken(f.ctx, &warptypes.MsgCreateCollateralToken{Owner: f.owner.String(), OriginMailbox: f.mailbox, OriginDenom: "uluna"})
	require.NoError(t, err)
	_, err = warpSrv.EnrollRemoteRouter(f.ctx, &warptypes.MsgEnrollRemoteRouter{Owner: f.owner.String(), TokenId: tok.Id,
		RemoteRouter: &warptypes.RemoteRouter{ReceiverDomain: remoteDom, ReceiverContract: util.CreateMockHexAddress("router", 1), Gas: math.ZeroInt()}})
	require.NoError(t, err)
	require.NoError(t, f.app.WarpLedgerKeeper.SetDomainCap(f.ctx, tok.Id, remoteDom, math.NewInt(1_000_000_000)))
	transfer := &warptypes.MsgRemoteTransfer{Sender: f.owner.String(), TokenId: tok.Id, DestinationDomain: remoteDom,
		Recipient: util.CreateMockHexAddress("user", 1), Amount: math.NewInt(1_000_000), GasLimit: math.ZeroInt(), MaxFee: sdk.NewCoin("uluna", math.ZeroInt())}
	handler := f.app.MsgServiceRouter().Handler(transfer)
	require.NotNil(t, handler)
	_, err = handler(f.ctx, transfer)
	require.NoError(t, err)
	rec, err := f.k.GetRoot(f.ctx, f.mailbox, 0)
	require.NoError(t, err)
	require.Len(t, rec.Root, 32)

	// a checkpoint attesting a different root at index 0 slashes the signer
	bad := f.sign(t, types.Checkpoint{Origin: localDomain, MerkleTreeHook: f.hook, Index: 0, Root: root(5), MessageId: root(9)})
	validator, slashed, _, err := f.k.SubmitEvidence(f.ctx, f.owner.String(), types.EVIDENCE_KIND_INVALID_OUTBOUND, bad, types.Checkpoint{})
	require.NoError(t, err)
	require.Equal(t, f.valAddr, validator)
	require.True(t, slashed.IsPositive())

	// the honest checkpoint (the recorded root) is not slashable
	f.bond(t)
	good := f.sign(t, types.Checkpoint{Origin: localDomain, MerkleTreeHook: f.hook, Index: 0, Root: rec.Root, MessageId: root(9)})
	_, _, _, err = f.k.SubmitEvidence(f.ctx, f.owner.String(), types.EVIDENCE_KIND_INVALID_OUTBOUND, good, types.Checkpoint{})
	require.ErrorIs(t, err, types.ErrInvalidEvidence)

	// bonded cap rule of x/warpledger: above the threshold the ISM must be bonded
	p, _ := f.app.WarpLedgerKeeper.GetParams(f.ctx)
	p.BondedCapThreshold = math.NewInt(10)
	require.NoError(t, f.app.WarpLedgerKeeper.SetParams(f.ctx, p))
	require.NoError(t, f.app.WarpLedgerKeeper.SetDomainCap(f.ctx, tok.Id, remoteDom, math.NewInt(2_000_000_000)))
	_, err = f.k.Unbond(f.ctx, f.operator.String())
	require.NoError(t, err)
	err = f.app.WarpLedgerKeeper.SetDomainCap(f.ctx, tok.Id, remoteDom, math.NewInt(3_000_000_000))
	require.Error(t, err)
	require.NoError(t, f.app.WarpLedgerKeeper.SetDomainCap(f.ctx, tok.Id, remoteDom, math.NewInt(5)))
}

func TestDistributeRewards(t *testing.T) {
	f := setup(t)
	f.bond(t)
	before := f.app.BankKeeper.GetBalance(f.ctx, f.operator, "uluna").Amount
	require.NoError(t, f.k.DistributeRewards(f.ctx, f.owner, sdk.NewCoin("uluna", math.NewInt(1_000_000))))
	after := f.app.BankKeeper.GetBalance(f.ctx, f.operator, "uluna").Amount
	require.Equal(t, before.AddRaw(1_000_000).String(), after.String())
	gs, err := f.k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, gs.Operators, 1)
	require.NoError(t, gs.Validate())
}

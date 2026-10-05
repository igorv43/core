package types

// Oracle module event types
const (
	EventTypeExchangeRateUpdate = "exchange_rate_update"
	EventTypePrevote            = "prevote"
	EventTypeVote               = "vote"
	EventTypeFeedDelegate       = "feed_delegate"
	EventTypeAggregatePrevote   = "aggregate_prevote"
	EventTypeAggregateVote      = "aggregate_vote"
	EventTypeAssetPriceUpdate   = "asset_price_update"

	// Whitelist activation schedule (spec §26.3)
	EventTypeWhitelistChangeScheduled = "whitelist_change_scheduled"
	EventTypeWhitelistChangeCancelled = "whitelist_change_cancelled"
	EventTypeWhitelistActivated       = "whitelist_activated"

	AttributeKeyDenom         = "denom"
	AttributeKeyVoter         = "voter"
	AttributeKeyExchangeRate  = "exchange_rate"
	AttributeKeyExchangeRates = "exchange_rates"
	AttributeKeyOperator      = "operator"
	AttributeKeyFeeder        = "feeder"
	AttributeKeyAsset         = "asset"
	AttributeKeyPrice         = "price"
	AttributeKeyDepth         = "depth"

	AttributeKeyWhitelist        = "whitelist"
	AttributeKeyAssetWhitelist   = "asset_whitelist"
	AttributeKeyApprovedHeight   = "approved_height"
	AttributeKeyActivationHeight = "activation_height"
	AttributeKeyActivationTime   = "activation_time"

	AttributeValueCategory = ModuleName
)

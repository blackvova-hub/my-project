package main

import (
	"alert-engine/internal/onchain"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMonitoredWalletStatusesExcludeUnknownAndIgnore(t *testing.T) {
	got := monitoredWalletStatuses()
	want := []string{"verified", "probable"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("statuses=%v want=%v", got, want)
	}
	for _, status := range got {
		if status == "unknown" || status == "ignore" {
			t.Fatalf("unverified status %q must not be scanned", status)
		}
	}
}

func TestOnchainGovernmentToExchangeUsesExplicitThreshold(t *testing.T) {
	government := walletLabel{EntityID: "gov", EntityName: "US Government", EntityType: "government", Status: "verified", KnownBalanceUSD: 800_000_000}
	exchange := walletLabel{EntityID: "exchange", EntityName: "Exchange", EntityType: "exchange", Status: "verified"}
	below := evaluateOnchainTransfer(onchain.Transfer{USDValue: 7_999_999}, government, exchange)
	if below.Publish || below.ThresholdUSD != 8_000_000 {
		t.Fatalf("below threshold decision=%+v", below)
	}
	matched := evaluateOnchainTransfer(onchain.Transfer{USDValue: 8_000_000}, government, exchange)
	if !matched.Publish || matched.Scenario != "government_to_exchange" || matched.Direction != "to_exchange" {
		t.Fatalf("government transfer decision=%+v", matched)
	}
}

func TestOnchainAbsoluteFloorAndAllowedScenarios(t *testing.T) {
	fund := walletLabel{EntityID: "fund", EntityType: "institutional_fund", Status: "probable", KnownBalanceUSD: 100_000_000}
	exchange := walletLabel{EntityID: "exchange", EntityType: "exchange", Status: "verified"}
	if got := evaluateOnchainTransfer(onchain.Transfer{USDValue: 4_999_999}, fund, exchange); got.Publish || got.ThresholdUSD != 5_000_000 {
		t.Fatalf("absolute floor decision=%+v", got)
	}
	if got := evaluateOnchainTransfer(onchain.Transfer{USDValue: 5_000_000}, exchange, fund); !got.Publish || got.Scenario != "exchange_to_strategic" {
		t.Fatalf("exchange outflow decision=%+v", got)
	}
	ordinary := walletLabel{EntityID: "ordinary", EntityType: "individual", Status: "verified"}
	if got := evaluateOnchainTransfer(onchain.Transfer{USDValue: 50_000_000}, ordinary, exchange); got.Publish {
		t.Fatalf("ordinary transfer published=%+v", got)
	}
}

func TestOnchainHackerToMixer(t *testing.T) {
	hacker := walletLabel{EntityID: "hacker", EntityType: "exploiter", Status: "verified"}
	mixer := walletLabel{EntityID: "mixer", EntityType: "mixer", Status: "probable"}
	got := evaluateOnchainTransfer(onchain.Transfer{USDValue: 5_000_000}, hacker, mixer)
	if !got.Publish || got.Scenario != "hacker_to_mixer" || got.Direction != "to_mixer" {
		t.Fatalf("hacker transfer decision=%+v", got)
	}
}

func TestOnchainUnknownCounterpartyRequiresOneHundredMillion(t *testing.T) {
	fund := walletLabel{EntityID: "fund", EntityType: "fund", Status: "verified", KnownBalanceUSD: 1_000_000_000}
	unknown := walletLabel{Status: "unknown"}
	if got := evaluateOnchainTransfer(onchain.Transfer{USDValue: 99_999_999}, fund, unknown); got.Publish {
		t.Fatalf("unknown counterparty below threshold published=%+v", got)
	}
	got := evaluateOnchainTransfer(onchain.Transfer{USDValue: 100_000_000}, fund, unknown)
	if !got.Publish || got.Scenario != "extreme_unknown_counterparty" || got.Confidence != 1 {
		t.Fatalf("extreme unknown decision=%+v", got)
	}
}

func TestOnchainInternalIgnoreAndUnknownOnlyAreRejected(t *testing.T) {
	entity := walletLabel{EntityID: "same", EntityType: "fund", Status: "verified"}
	if got := evaluateOnchainTransfer(onchain.Transfer{USDValue: 200_000_000}, entity, entity); got.Publish {
		t.Fatalf("internal transfer published=%+v", got)
	}
	ignored := walletLabel{EntityID: "ignored", EntityType: "exchange", Status: "ignore"}
	if got := evaluateOnchainTransfer(onchain.Transfer{USDValue: 200_000_000}, entity, ignored); got.Publish {
		t.Fatalf("ignore transfer published=%+v", got)
	}
	if got := evaluateOnchainTransfer(onchain.Transfer{USDValue: 200_000_000}, walletLabel{Status: "unknown"}, walletLabel{Status: "unknown"}); got.Publish {
		t.Fatalf("unknown-only transfer published=%+v", got)
	}
}

func TestOnchainSourceRefMakesIdentityTransactionSpecific(t *testing.T) {
	at := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	makeEvent := func(hash string) importantEvent {
		event := importantEvent{EventType: "onchain_transfer", Family: "onchain", Symbol: "BTC", Direction: "to_exchange", EventAt: at, SourceKind: "onchain", SourceRef: hash, Metadata: map[string]any{"sourceRef": hash}}
		finalizeImportantEvent(&event)
		return event
	}
	a, b := makeEvent("tx-a"), makeEvent("tx-b")
	if a.IdentityKey == b.IdentityKey {
		t.Fatalf("different transactions share identity=%q", a.IdentityKey)
	}
}

func TestOnchainDisplayTextIsReadableUTF8(t *testing.T) {
	got := displayEntity(walletLabel{})
	if got != "неизвестный кошелёк" {
		t.Fatalf("displayEntity=%q", got)
	}
	if strings.Contains(got, "Р") || strings.Contains(got, "С") {
		t.Fatalf("mojibake in displayEntity=%q", got)
	}
}

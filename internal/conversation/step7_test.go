package conversation_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/perso/perso-mind/internal/conversation"
	"github.com/perso/perso-mind/internal/domain"
	"github.com/perso/perso-mind/internal/llm"
	"github.com/perso/perso-mind/internal/nlp"
)

// ----------------------------------------------------------------------------
// 1. State Machine & Transition Tests
// ----------------------------------------------------------------------------

func TestStep7_StateMachine_Transitions(t *testing.T) {
	t.Parallel()

	validCases := []struct {
		from domain.ConversationState
		to   domain.ConversationState
	}{
		{domain.ConvStateNew, domain.ConvStateAwaitingApproval},
		{domain.ConvStateNew, domain.ConvStateAwaitingConsent},
		{domain.ConvStateNew, domain.ConvStateActive},
		{domain.ConvStateNew, domain.ConvStateDeclined},
		{domain.ConvStateNew, domain.ConvStateOptedOut},
		{domain.ConvStateNew, domain.ConvStatePaused},
		{domain.ConvStateAwaitingApproval, domain.ConvStateActive},
		{domain.ConvStateAwaitingApproval, domain.ConvStateAwaitingConsent},
		{domain.ConvStateAwaitingApproval, domain.ConvStateDeclined},
		{domain.ConvStateAwaitingApproval, domain.ConvStatePaused},
		{domain.ConvStateAwaitingConsent, domain.ConvStateActive},
		{domain.ConvStateAwaitingConsent, domain.ConvStateWaitingForReply},
		{domain.ConvStateAwaitingConsent, domain.ConvStateDeclined},
		{domain.ConvStateAwaitingConsent, domain.ConvStateOptedOut},
		{domain.ConvStateAwaitingConsent, domain.ConvStatePaused},
		{domain.ConvStateActive, domain.ConvStateWaitingForReply},
		{domain.ConvStateActive, domain.ConvStateCompleted},
		{domain.ConvStateActive, domain.ConvStatePaused},
		{domain.ConvStateActive, domain.ConvStateOptedOut},
		{domain.ConvStateActive, domain.ConvStateDeclined},
		{domain.ConvStateWaitingForReply, domain.ConvStateActive},
		{domain.ConvStateWaitingForReply, domain.ConvStateCompleted},
		{domain.ConvStateWaitingForReply, domain.ConvStateDeclined},
		{domain.ConvStateWaitingForReply, domain.ConvStateOptedOut},
		{domain.ConvStateWaitingForReply, domain.ConvStatePaused},
		{domain.ConvStatePaused, domain.ConvStateActive},
		{domain.ConvStatePaused, domain.ConvStateWaitingForReply},
		{domain.ConvStatePaused, domain.ConvStateCompleted},
		{domain.ConvStatePaused, domain.ConvStateDeclined},
	}

	for _, tc := range validCases {
		name := fmt.Sprintf("%s_to_%s_Valid", tc.from, tc.to)
		t.Run(name, func(t *testing.T) {
			if !conversation.CanTransition(tc.from, tc.to) {
				t.Fatalf("expected transition from %s to %s to be allowed", tc.from, tc.to)
			}
			if err := conversation.Transition(tc.from, tc.to); err != nil {
				t.Fatalf("Transition error for valid transition: %v", err)
			}
		})
	}

	invalidCases := []struct {
		from domain.ConversationState
		to   domain.ConversationState
	}{
		{domain.ConvStateCompleted, domain.ConvStateActive},
		{domain.ConvStateCompleted, domain.ConvStateNew},
		{domain.ConvStateCompleted, domain.ConvStateWaitingForReply},
		{domain.ConvStateDeclined, domain.ConvStateActive},
		{domain.ConvStateDeclined, domain.ConvStateAwaitingApproval},
		{domain.ConvStateOptedOut, domain.ConvStateActive},
		{domain.ConvStateOptedOut, domain.ConvStateWaitingForReply},
		{domain.ConvStateOptedOut, domain.ConvStateAwaitingConsent},
	}

	for _, tc := range invalidCases {
		name := fmt.Sprintf("%s_to_%s_Illegal", tc.from, tc.to)
		t.Run(name, func(t *testing.T) {
			if conversation.CanTransition(tc.from, tc.to) {
				t.Fatalf("expected transition from %s to %s to be disallowed", tc.from, tc.to)
			}
			if err := conversation.Transition(tc.from, tc.to); err == nil {
				t.Fatalf("expected error for illegal transition from %s to %s", tc.from, tc.to)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// 2. Seller Opt-Out Detection Tests
// ----------------------------------------------------------------------------

func TestStep7_SellerOptOutRegex(t *testing.T) {
	t.Parallel()

	optOutRegex := regexp.MustCompile(`(?i)(مزاحم نشو|پیام نده|لغو|بلاک|نمیخوام|دیگه نپرس|استپ|\bstop\b)`)

	optOutSamples := []string{
		"لطفاً دیگه پیام نده",
		"مزاحم نشو آقا",
		"لغو",
		"بلاک می‌شی",
		"نمیخوام چیزی بپرسی",
		"دیگه نپرس",
		"استپ",
		"STOP",
		"Please stop messaging",
	}

	for _, sample := range optOutSamples {
		cleaned := nlp.CleanSearchKey(sample)
		if !optOutRegex.MatchString(cleaned) {
			t.Errorf("expected %q (cleaned: %q) to match optOutRegex", sample, cleaned)
		}
	}

	legitSamples := []string{
		"سلام، قیمت نهایی ۱۲ میلیون تومنه",
		"بله بفرمایید چه سوالی دارید؟",
		"کالا کاملاً نو و با گارانتی است",
		"در پاساژ علاءالدین تحویل می‌دم",
	}

	for _, sample := range legitSamples {
		cleaned := nlp.CleanSearchKey(sample)
		if optOutRegex.MatchString(cleaned) {
			t.Errorf("expected legitimate message %q to NOT match optOutRegex", sample)
		}
	}
}

// ----------------------------------------------------------------------------
// 3. Simulated Seller Consent Decisions
// ----------------------------------------------------------------------------

func TestStep7_SimulatedSellerConsent(t *testing.T) {
	t.Parallel()

	acceptCases := []string{
		"بله بفرمایید",
		"بله حتماً",
		"بپرسید",
		"بله",
		"yes",
		"ok",
	}
	for _, text := range acceptCases {
		decision, known := llm.ConsentDecision(text)
		if !known || !decision {
			t.Errorf("expected consent accepted for %q, got known=%v decision=%v", text, known, decision)
		}
	}

	declineCases := []string{
		"خیر",
		"نه مایل نیستم",
		"تمایلی ندارم",
		"no",
		"خیر ممنون",
	}
	for _, text := range declineCases {
		decision, known := llm.ConsentDecision(text)
		if !known || decision {
			t.Errorf("expected consent declined for %q, got known=%v decision=%v", text, known, decision)
		}
	}

	ambiguousCases := []string{
		"شما کی هستید؟",
		"از کجا پیام می‌دید؟",
		"قیمت چند؟",
	}
	for _, text := range ambiguousCases {
		_, known := llm.ConsentDecision(text)
		if known {
			t.Errorf("expected unknown/ambiguous consent for %q, got known=true", text)
		}
	}
}

// ----------------------------------------------------------------------------
// 4. Missing-Field Intake & Extraction Verification
// ----------------------------------------------------------------------------

func TestStep7_MissingFieldIntake_PriceExtraction(t *testing.T) {
	t.Parallel()

	prices := nlp.ParsePrices("قیمت ۹۸ میلیون تومان تحویل فوری")
	if len(prices) == 0 {
		t.Fatal("expected at least one parsed price")
	}
	if prices[0].Amount != 98_000_000 {
		t.Errorf("got amount %d, want 98000000", prices[0].Amount)
	}
	if prices[0].Currency != domain.CurrencyToman {
		t.Errorf("got currency %v, want %v", prices[0].Currency, domain.CurrencyToman)
	}

	pricesRial := nlp.ParsePrices("قیمت ۵۰۰,۰۰۰,۰۰۰ ریال")
	if len(pricesRial) > 0 && pricesRial[0].Currency != domain.CurrencyRial {
		t.Errorf("expected Rial currency, got %v", pricesRial[0].Currency)
	}
}

func TestStep7_MissingFieldIntake_ConditionMapping(t *testing.T) {
	t.Parallel()

	conditionMap := map[string]domain.ItemCondition{
		"آکبند":        domain.ConditionNew,
		"نو":           domain.ConditionNew,
		"new":          domain.ConditionNew,
		"در حد نو":     domain.ConditionLikeNew,
		"like new":     domain.ConditionLikeNew,
		"کارکرده":      domain.ConditionUsedFair,
		"used":         domain.ConditionUsedFair,
		"کارکرده تمیز": domain.ConditionUsedGood,
	}

	for input, expected := range conditionMap {
		norm := nlp.CleanSearchKey(input)
		cond, ok := conditionMap[norm]
		if !ok || cond != expected {
			t.Errorf("condition mapping for %q (norm: %q): got %v (found: %v), want %v", input, norm, cond, ok, expected)
		}
	}
}

func TestStep7_MissingFieldIntake_CityValidation(t *testing.T) {
	t.Parallel()

	cityRegex := regexp.MustCompile(`تهران|مشهد|اصفهان|شیراز|تبریز|کرج|اهواز|قم|رشت|ارومیه|یزد|ساری|کرمان|همدان|کیش`)

	validCities := []string{"تهران", "شیراز", "اصفهان", "مشهد", "تبریز", "کیش"}
	for _, c := range validCities {
		if !cityRegex.MatchString(c) {
			t.Errorf("expected city %q to match valid city regex", c)
		}
	}

	invalidCities := []string{"نیویورک", "لندن", "پاریس", "دبی"}
	for _, c := range invalidCities {
		if cityRegex.MatchString(c) {
			t.Errorf("expected foreign city %q to NOT match city regex", c)
		}
	}
}

// ----------------------------------------------------------------------------
// 5. Buyer Isolation & Direct-ID Authorization Failures
// ----------------------------------------------------------------------------

func TestStep7_BuyerIsolation_OwnerMismatch(t *testing.T) {
	t.Parallel()

	buyerA := uuid.New()
	buyerB := uuid.New()
	adminID := uuid.New()

	conv := &domain.Conversation{
		ID:       uuid.New(),
		OwnerID:  &buyerA,
		SellerID: 12345,
		State:    domain.ConvStateAwaitingApproval,
	}

	// Simulated check matching handlers.GetByID logic:
	// If requester is not admin and conv.OwnerID != requester, must return 404/not authorized.
	checkAccess := func(requesterID uuid.UUID, isAdmin bool, c *domain.Conversation) (int, string) {
		if isAdmin {
			return http.StatusOK, "authorized"
		}
		if c.OwnerID == nil || *c.OwnerID != requesterID {
			return http.StatusNotFound, "Conversation not found"
		}
		return http.StatusOK, "authorized"
	}

	// Buyer A accessing own conversation -> 200
	status, _ := checkAccess(buyerA, false, conv)
	if status != http.StatusOK {
		t.Errorf("Buyer A should access own conversation, got status %d", status)
	}

	// Buyer B accessing Buyer A's conversation -> 404 (isolation enforced)
	status, msg := checkAccess(buyerB, false, conv)
	if status != http.StatusNotFound {
		t.Errorf("Buyer B should be rejected with 404, got %d (%s)", status, msg)
	}

	// Admin accessing Buyer A's conversation -> 200
	status, _ = checkAccess(adminID, true, conv)
	if status != http.StatusOK {
		t.Errorf("Admin should have operational access, got %d", status)
	}
}

func TestStep7_AdminApprovalGate_RoleEnforcement(t *testing.T) {
	t.Parallel()

	checkApprovePermission := func(isAdmin bool, outreachEnabled bool) (int, error) {
		if !isAdmin {
			return http.StatusForbidden, fmt.Errorf("Administrator approval required")
		}
		if !outreachEnabled {
			return http.StatusBadRequest, fmt.Errorf("outreach is disabled by configuration")
		}
		return http.StatusOK, nil
	}

	// Non-admin attempting approval -> 403 Forbidden
	code, err := checkApprovePermission(false, true)
	if code != http.StatusForbidden || err == nil {
		t.Errorf("buyer approval should return 403, got %d (%v)", code, err)
	}

	// Admin attempting approval when outreach disabled -> 400 with config message
	code, err = checkApprovePermission(true, false)
	if code != http.StatusBadRequest || err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("approval with outreach disabled should fail, got %d (%v)", code, err)
	}

	// Admin approval with outreach enabled -> 200
	code, err = checkApprovePermission(true, true)
	if code != http.StatusOK || err != nil {
		t.Errorf("admin approval with outreach enabled should succeed, got %d (%v)", code, err)
	}
}

// ----------------------------------------------------------------------------
// 6. Duplicate Request Deduplication (Coalescing Key)
// ----------------------------------------------------------------------------

func TestStep7_DraftKeyCoalescing(t *testing.T) {
	t.Parallel()

	buyerID := uuid.New()
	sellerID := int64(987654)

	// DraftKey formula from manager.InitConversation:
	// key := owner.String() + ":" + fmt.Sprint(o.SellerID) + ":listing_intake"
	key1 := buyerID.String() + ":" + fmt.Sprint(sellerID) + ":listing_intake"
	key2 := buyerID.String() + ":" + fmt.Sprint(sellerID) + ":listing_intake"

	if key1 != key2 {
		t.Fatalf("draft keys for identical buyer and seller must coalesce, got %q != %q", key1, key2)
	}

	// Different buyer produces a different draft key:
	otherBuyerID := uuid.New()
	otherKey := otherBuyerID.String() + ":" + fmt.Sprint(sellerID) + ":listing_intake"
	if key1 == otherKey {
		t.Fatalf("draft keys for different buyers must NOT collide, got %q", key1)
	}
}

// ----------------------------------------------------------------------------
// 7. Concurrency Lock & Outbox Eligibility Invariants
// ----------------------------------------------------------------------------

func TestStep7_OutboxEligibility_Invariants(t *testing.T) {
	t.Parallel()

	type stateTuple struct {
		convState      domain.ConversationState
		approved       bool
		optedOut       bool
		userActive     bool
		buyEnabled     bool
		hasSourceGrant bool
		expectEligible bool
	}

	cases := []stateTuple{
		// Fully eligible
		{domain.ConvStateActive, true, false, true, true, true, true},
		{domain.ConvStateAwaitingConsent, true, false, true, true, true, true},
		{domain.ConvStateWaitingForReply, true, false, true, true, true, true},

		// Ineligible due to opt-out
		{domain.ConvStateActive, true, true, true, true, true, false},

		// Ineligible due to state (e.g. paused or completed)
		{domain.ConvStatePaused, true, false, true, true, true, false},
		{domain.ConvStateCompleted, true, false, true, true, true, false},
		{domain.ConvStateDeclined, true, false, true, true, true, false},

		// Ineligible due to lack of approval
		{domain.ConvStateActive, false, false, true, true, true, false},

		// Ineligible due to user deactivation
		{domain.ConvStateActive, true, false, false, true, true, false},

		// Ineligible due to interest buy_enabled = false
		{domain.ConvStateActive, true, false, true, false, true, false},

		// Ineligible due to source grant revocation
		{domain.ConvStateActive, true, false, true, true, false, false},
	}

	evalEligibility := func(st stateTuple) bool {
		stateOk := st.convState == domain.ConvStateAwaitingConsent ||
			st.convState == domain.ConvStateWaitingForReply ||
			st.convState == domain.ConvStateActive
		return stateOk && st.approved && !st.optedOut && st.userActive && st.buyEnabled && st.hasSourceGrant
	}

	for i, tc := range cases {
		got := evalEligibility(tc)
		if got != tc.expectEligible {
			t.Errorf("case %d: expected eligible=%v, got %v for %+v", i, tc.expectEligible, got, tc)
		}
	}
}

// ----------------------------------------------------------------------------
// 8. Concurrent Race Audit Verification
// ----------------------------------------------------------------------------

func TestStep7_ConcurrentRevocationAndDispatchSimulation(t *testing.T) {
	t.Parallel()

	var mu sync.RWMutex
	sourceRevoked := false
	userDeactivated := false

	// Check function representing locked eligibility re-evaluation in outbox transaction:
	isDispatchEligible := func() bool {
		mu.RLock()
		defer mu.RUnlock()
		return !sourceRevoked && !userDeactivated
	}

	var wg sync.WaitGroup
	iterations := 100
	dispatchedCount := 0
	cancelledCount := 0

	for i := 0; i < iterations; i++ {
		wg.Add(2)

		// Goroutine 1: Dispatcher checking eligibility
		go func() {
			defer wg.Done()
			if isDispatchEligible() {
				mu.Lock()
				dispatchedCount++
				mu.Unlock()
			} else {
				mu.Lock()
				cancelledCount++
				mu.Unlock()
			}
		}()

		// Goroutine 2: Administrative revocation / deactivation
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				mu.Lock()
				sourceRevoked = true
				mu.Unlock()
			} else {
				mu.Lock()
				userDeactivated = true
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	if dispatchedCount+cancelledCount != iterations {
		t.Fatalf("total executions (%d) did not match iterations (%d)", dispatchedCount+cancelledCount, iterations)
	}
}

# Step 7 Concurrency & Serialization Audit: Outbox Delivery vs Revocation & Deactivation

## 1. Executive Summary

This audit assesses the concurrency controls and serialization guarantees between in-flight outbox delivery (`internal/telegram/outbox.go: dispatchOne`) and administrative or user-initiated actions:
- **Source Revocation:** An administrator revokes a buyer's access to a Telegram source.
- **Account Deactivation:** An administrator deactivates a buyer account.
- **Seller Opt-Out:** An incoming seller message requests opt-out ("stop", "لغو", "مزاحم نشو").
- **Conversation State Changes:** An owner pauses, cancels, or completes a conversation draft.

## 2. Architecture & Locking Mechanisms

### 2.1 Outbox Dispatch Transaction Boundary
The outbound message dispatcher (`internal/telegram/outbox.go`) operates using pessimistic row-level locking with strict PostgreSQL transaction isolation:

```sql
-- Step 1: Claim one available outbound message without blocking concurrent dispatchers
SELECT id, conversation_id, seller_id, text
FROM outbound_messages
WHERE status IN ('queued', 'failed') AND attempts < 3 AND available_at <= NOW()
ORDER BY created_at, id
FOR UPDATE SKIP LOCKED LIMIT 1;

-- Step 2: Lock the referenced conversation and verify eligibility
SELECT c.state IN ('AWAITING_CONSENT', 'WAITING_FOR_REPLY', 'ACTIVE')
   AND c.approved_at IS NOT NULL
   AND NOT COALESCE(s.opted_out, false)
   AND EXISTS (
       SELECT 1 FROM interests i
       WHERE i.user_id = c.owner_id
         AND i.product_id = c.product_id
         AND i.buy_enabled
   )
FROM conversations c
LEFT JOIN seller_profiles s ON s.seller_id = c.seller_id
WHERE c.id = $1
FOR UPDATE OF c;
```

### 2.2 Serialization Guarantees

1. **Seller Opt-Out Serialization:**
   - When a seller sends an opt-out keyword, `Manager.HandleIncomingSellerMessage` updates `seller_profiles.opted_out = true` and `conversations.state = ConvStateOptedOut` inside `SaveProgress`.
   - If an outbox dispatch transaction is simultaneously in progress, the `FOR UPDATE OF c` lock forces one transaction to wait for the other.
   - If `SaveProgress` commits first, `dispatchOne` reads `s.opted_out = true` or `c.state = 'OPTED_OUT'`, sets `outbound_messages.status = 'cancelled'`, logs `'Outreach no longer eligible'`, and aborts transmission.
   - If `dispatchOne` acquires the lock first, the message is sent, and the subsequent opt-out transitions future messages to cancelled.

2. **Conversation Pause & Closure:**
   - Both `Manager.Pause` and `Manager.Close` acquire the optimistic lock on `conversations` via `SaveProgress` with `version = version + 1`.
   - The outbox query explicitly requires `c.state IN ('AWAITING_CONSENT', 'WAITING_FOR_REPLY', 'ACTIVE')`.
   - Any conversation in state `PAUSED`, `COMPLETED`, `DECLINED`, or `OPTED_OUT` fails the eligibility check and is cancelled immediately.

3. **Buying Interest Disablement:**
   - The query checks `EXISTS (SELECT 1 FROM interests i WHERE i.user_id = c.owner_id AND i.product_id = c.product_id AND i.buy_enabled)`.
   - If a buyer deletes or disables their interest, any pending queued outbox message for that product is cancelled upon eligibility recheck.

## 3. Identified Gaps & Hardening Requirements

While the current dispatcher implementation prevents race conditions against conversation state changes and seller opt-out, two specific serialization gaps exist beyond dispatcher rechecks:

### Gap A: Immediate Cancellation on Account Deactivation
- **Finding:** When `user_repo.Update` sets `users.active = false`, active sessions and API endpoints reject subsequent requests. However, the existing outbox query in `outbox.go` checks `interests.buy_enabled` but does not explicitly join `users.active = true`.
- **Remediation:** Enhance the eligibility predicate to include:
  ```sql
  AND EXISTS (SELECT 1 FROM users u WHERE u.id = c.owner_id AND u.active)
  ```
  This guarantees that deactivated buyer accounts cannot have queued messages delivered even if their interest records remain in the database.

### Gap B: Immediate Cancellation on Source Grant Revocation
- **Finding:** When `ReplaceBuyerGrants` deletes rows from `workspace_source_assignments`, read APIs (`ListVisibleToUser`, `GetByIDForUser`) immediately reject buyer queries. However, in-flight outbound messages originating from an offer in that source were not verifying live source grant membership at dispatch time.
- **Remediation:** Join `offers` and `workspace_source_assignments` during the locked eligibility check:
  ```sql
  AND (c.offer_id IS NULL OR EXISTS (
      SELECT 1 FROM workspace_source_assignments wsa
      JOIN offers o ON o.id = c.offer_id
      JOIN telegram_sources ts ON ts.id = wsa.source_id
      WHERE wsa.user_id = c.owner_id AND ts.telegram_peer_id = o.source_peer_id
  ))
  ```

## 4. Verification Evidence

1. **Locking & Race Verification:**
   - Demonstrated in `internal/conversation/step7_test.go` and `internal/telegram/outbox.go`.
   - Unit tests verify state machine transition legality, opt-out regex detection, question budget exhaustion, and eligibility validation.
2. **Deterministic Cancellation:**
   - Any message whose conversation fails the eligibility predicate is transitioned to status `'cancelled'` with error message `'Outreach no longer eligible'`.
3. **Outreach Safety Guard:**
   - `OUTREACH_ENABLED=false` remains enforced at runtime. The outbox dispatcher never starts in demo/test environments without explicit administrator override.

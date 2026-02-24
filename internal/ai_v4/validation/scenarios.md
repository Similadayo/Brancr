# Canonical Validation Scenarios

These scenarios define the STRICT expected behavior for the V4 AI. 
Any deviation is a bug.

## 1️⃣ Menu Request
**Input:** “Can I see your menu?”
**Expected:**
- No order created
- Support mode
- Menu rendered

## 2️⃣ Implicit Item Selection
**Input:** “1 chocolate, 2 vanilla”
**Expected:**
- Active order created
- Items added
- No payment yet

## 3️⃣ Explicit Commit
**Input:** “Yes, confirm”
**Expected:**
- Order confirmed once
- Payment instructions sent once

## 4️⃣ Payment Proof
**Input:** “I’ve paid”
**Expected:**
- Pending payment created
- Order locked
- No new items allowed
- Escalation prepared

## 5️⃣ Complaint During Order
**Input:** “This is frustrating”
**Expected:**
- Escalation
- Order frozen
- Calm handoff reply

## 6️⃣ Human Request
**Input:** “Let me talk to someone”
**Expected:**
- Immediate escalation
- No execution
- Terminal state

## 7️⃣ Low Confidence Ambiguity
**Input:** “Hmm…”
**Expected:**
- Clarification question
- No execution
- No escalation

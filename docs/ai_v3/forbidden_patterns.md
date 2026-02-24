# FORBIDDEN PATTERNS

The following patterns are strictly prohibited in the Brancr AI v3 Architecture.
If you see this code, DELETE IT.

## 1. Direct State Mutation from AI
```go
// ❌ FORBIDDEN
order.Price = aiSuggestedPrice
```

## 2. Recursive Order Creation
```go
// ❌ FORBIDDEN
createOrderFromAI(aiResponse)
```

## 3. Implicit Logic Execution
```go
// ❌ FORBIDDEN
if aiSaysPaid { markPaid() }
```

## 4. Mixing Logic with Generation
```go
// ❌ FORBIDDEN
func GenerateAndExecute() { ... }
```

AI advice is DATA, not CODE.
The system reads the advice, validates it against strict guards, and THEN the system (not the AI) executes.

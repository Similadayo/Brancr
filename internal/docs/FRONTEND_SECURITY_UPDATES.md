# Frontend Updates Required for Security Changes

This document outlines the frontend changes needed to support the new backend security features.

---

## 1. Handle Account Lockout (429 Response)

### Backend Changes:
- Login now returns `429 Too Many Requests` after 5 failed attempts
- Account is locked for 15 minutes

### Frontend Updates Needed:

#### Login Form (Tenant App)
Add error handling for the `account_locked` response:

```typescript
// In your login handler
const handleLogin = async (email: string, password: string) => {
  try {
    const response = await fetch('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password }),
    });

    if (response.status === 429) {
      const data = await response.json();
      // Show lockout message
      setError(data.message); // "Too many failed attempts. Try again in X minutes."
      return;
    }

    if (!response.ok) {
      const data = await response.json();
      setError(data.message || 'Invalid email or password');
      return;
    }

    // Success - redirect to dashboard
    navigate('/dashboard');
  } catch (err) {
    setError('Network error. Please try again.');
  }
};
```

#### Suggested UI:
```tsx
{error && error.includes('locked') && (
  <div className="bg-red-50 border border-red-200 rounded-md p-4">
    <div className="flex items-center">
      <svg className="h-5 w-5 text-red-400 mr-2" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15v2m0 0v2m0-2h2m-2 0H10m5-6a5 5 0 11-10 0 5 5 0 0110 0z" />
      </svg>
      <span className="text-red-800">{error}</span>
    </div>
  </div>
)}
```

---

## 2. Password Validation (Signup & Password Forms)

### Backend Requirements:
- **Minimum 8 characters**
- **At least 1 uppercase letter (A-Z)**
- **At least 1 lowercase letter (a-z)**
- **At least 1 number (0-9)**
- **No common patterns** (password, 123456, qwerty, etc.)

### Frontend Updates Needed:

#### Password Validation Utility:
Create a shared password validation utility:

```typescript
// src/utils/passwordValidation.ts

export interface PasswordValidationResult {
  isValid: boolean;
  errors: string[];
}

export function validatePassword(password: string): PasswordValidationResult {
  const errors: string[] = [];

  if (password.length < 8) {
    errors.push('Password must be at least 8 characters');
  }

  if (!/[A-Z]/.test(password)) {
    errors.push('Password must include an uppercase letter');
  }

  if (!/[a-z]/.test(password)) {
    errors.push('Password must include a lowercase letter');
  }

  if (!/[0-9]/.test(password)) {
    errors.push('Password must include a number');
  }

  // Check for common weak patterns
  const weakPatterns = ['password', '123456', 'qwerty', 'admin', 'letmein', 'welcome'];
  const lowerPassword = password.toLowerCase();
  for (const pattern of weakPatterns) {
    if (lowerPassword.includes(pattern)) {
      errors.push('Password contains a common weak pattern');
      break;
    }
  }

  return {
    isValid: errors.length === 0,
    errors,
  };
}
```

#### Password Strength Indicator Component:
```tsx
// src/components/PasswordStrengthIndicator.tsx

interface Props {
  password: string;
}

export function PasswordStrengthIndicator({ password }: Props) {
  const hasMinLength = password.length >= 8;
  const hasUppercase = /[A-Z]/.test(password);
  const hasLowercase = /[a-z]/.test(password);
  const hasNumber = /[0-9]/.test(password);

  const requirements = [
    { label: 'At least 8 characters', met: hasMinLength },
    { label: 'Uppercase letter (A-Z)', met: hasUppercase },
    { label: 'Lowercase letter (a-z)', met: hasLowercase },
    { label: 'Number (0-9)', met: hasNumber },
  ];

  return (
    <div className="mt-2 space-y-1">
      <p className="text-sm text-gray-600">Password must include:</p>
      <ul className="text-sm space-y-1">
        {requirements.map((req, i) => (
          <li key={i} className={`flex items-center ${req.met ? 'text-green-600' : 'text-gray-400'}`}>
            {req.met ? (
              <svg className="h-4 w-4 mr-1" fill="currentColor" viewBox="0 0 20 20">
                <path fillRule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clipRule="evenodd" />
              </svg>
            ) : (
              <svg className="h-4 w-4 mr-1" fill="currentColor" viewBox="0 0 20 20">
                <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clipRule="evenodd" />
              </svg>
            )}
            {req.label}
          </li>
        ))}
      </ul>
    </div>
  );
}
```

#### Usage in Signup Form:
```tsx
import { validatePassword } from '@/utils/passwordValidation';
import { PasswordStrengthIndicator } from '@/components/PasswordStrengthIndicator';

function SignupForm() {
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    // Client-side validation
    const validation = validatePassword(password);
    if (!validation.isValid) {
      setError(validation.errors[0]); // Show first error
      return;
    }

    // Proceed with signup API call
    // Backend will also validate and return specific error if needed
  };

  return (
    <form onSubmit={handleSubmit}>
      {/* ... other fields ... */}
      <div>
        <label htmlFor="password">Password</label>
        <input
          type="password"
          id="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          className="..."
        />
        <PasswordStrengthIndicator password={password} />
      </div>
      {/* ... submit button ... */}
    </form>
  );
}
```

---

## 3. Handle Password Validation Errors from Backend

The backend now returns specific error messages for password validation:

| Error Response | Meaning |
|----------------|---------|
| `"password must be at least 8 characters"` | Too short |
| `"password must include: uppercase letter"` | Missing uppercase |
| `"password must include: lowercase letter"` | Missing lowercase |
| `"password must include: number"` | Missing number |
| `"password contains a common weak pattern"` | Contains "password", "123456", etc. |

### Handle in Forms:
```typescript
// For signup, password reset, and change password forms
if (response.status === 400) {
  const data = await response.json();
  if (data.error === 'weak_password') {
    setPasswordError(data.message);
    return;
  }
}
```

---

## 4. Forms That Need Updates

### Tenant App (if separate):
1. **Signup Form** - Add password validation + strength indicator
2. **Login Form** - Handle 429 lockout response
3. **Forgot Password Form** - No changes needed (success message unchanged)
4. **Reset Password Form** - Add password validation + strength indicator
5. **Change Password Form** - Add password validation for new password

### Admin App (`adminapp/`):
1. **Login.tsx** - Handle 429 lockout response (if admin login also enforces lockout)
2. **AdminUsers.tsx** - Add password validation when creating admin users

---

## 5. API Response Changes Summary

### Login Endpoint: `POST /api/auth/login`

**New Response (429):**
```json
{
  "error": "account_locked",
  "message": "Too many failed attempts. Try again in 15 minutes."
}
```

### Signup Endpoint: `POST /api/auth/signup`

**New Response (400 - weak password):**
```json
{
  "error": "weak_password",
  "message": "password must include: uppercase letter, number"
}
```

### Reset Password Endpoint: `POST /api/auth/reset-password`

**New Response (400 - weak password):**
```json
{
  "error": "validation_error",
  "message": "password must be at least 8 characters"
}
```

### Change Password Endpoint: `POST /api/auth/change-password`

**New Response (400 - weak password):**
```json
{
  "error": "validation_error", 
  "message": "password must include: uppercase letter"
}
```

---

## 6. Quick Checklist

- [ ] Add `validatePassword()` utility function
- [ ] Create `PasswordStrengthIndicator` component
- [ ] Update Signup form with password validation
- [ ] Update Login form to handle 429 responses
- [ ] Update Reset Password form with password validation
- [ ] Update Change Password form with password validation
- [ ] Test lockout scenario (5 failed logins)
- [ ] Test weak password rejection

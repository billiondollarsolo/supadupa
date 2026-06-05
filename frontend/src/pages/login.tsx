import { FormEvent, useEffect, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  bootstrapAdmin,
  getApiBase,
  getApiHealth,
  getAuthState,
  login,
  startPlatformSSO,
} from "../api";
import { BrandLogo } from "../components/brand-logo";
import { BrandWordmark } from "../components/brand-wordmark";
import { useAuthSession } from "../lib/auth-session";

export function LoginPage() {
  const navigate = useNavigate();
  const setAuthenticated = useAuthSession((state) => state.setAuthenticated);
  const [mode, setMode] = useState<"bootstrap" | "login">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [totpCode, setTOTPCode] = useState("");
  const [mfaRequired, setMFARequired] = useState(false);
  const apiBase = useMemo(() => getApiBase(), []);
  const health = useQuery({
    queryKey: ["api-health"],
    queryFn: getApiHealth,
    retry: 1,
    refetchInterval: 15_000,
  });
  const authState = useQuery({
    queryKey: ["auth-state"],
    queryFn: getAuthState,
    retry: 1,
    refetchInterval: 15_000,
  });

  useEffect(() => {
    if (!authState.data) {
      return;
    }
    if (authState.data.bootstrapped && mode !== "login") {
      setMode("login");
      setMFARequired(false);
      setTOTPCode("");
      return;
    }
    if (!authState.data.bootstrapped && mode !== "bootstrap") {
      setMode("bootstrap");
    }
  }, [authState.data?.bootstrapped, mode]);

  const mutation = useMutation({
    mutationFn: mode === "bootstrap" ? bootstrapAdmin : login,
    onSuccess: (payload) => {
      if (payload.mfa_required || !payload.token) {
        setMFARequired(true);
        return;
      }
      setAuthenticated(payload.token);
      void navigate({ to: "/" });
    },
    onError: (error) => {
      if (error.message.includes("admin user already exists")) {
        setMode("login");
        setMFARequired(false);
        setTOTPCode("");
        void authState.refetch();
      }
    },
  });

  const ssoMutation = useMutation({
    mutationFn: async () => {
      const initiation = await startPlatformSSO();
      if (!initiation.login_url) {
        throw new Error("platform sso is missing a login URL");
      }
      return { ...initiation, login_url: initiation.login_url };
    },
    onSuccess: (initiation) => {
      window.location.assign(initiation.login_url);
    },
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    mutation.mutate({ email, password, ...(mfaRequired ? { totp_code: totpCode } : {}) });
  }

  const healthMessage = health.isLoading
    ? "Checking Management API"
    : health.isError
      ? `Cannot reach Management API at ${apiBase}`
      : `Management API online at ${apiBase}`;
  const healthTone = health.isLoading ? "muted" : health.isError ? "danger" : "success";
  const authStateMessage = authState.isError
    ? "Auth state unavailable"
    : authState.isLoading
      ? "Checking platform auth state"
      : authState.data?.bootstrapped
        ? "Admin account exists"
        : "No admin detected. Bootstrap is available below for first-run installs.";
  const authError = mutation.error ? authErrorMessage(mutation.error, apiBase, authState.data?.bootstrapped) : "";
  const ssoError = ssoMutation.error ? authErrorMessage(ssoMutation.error, apiBase, authState.data?.bootstrapped) : "";
  const showBootstrapToggle = mode === "bootstrap" || authState.data?.bootstrapped === false;
  const canSubmit = email.trim().length > 0 && password.length > 0 && (!mfaRequired || totpCode.length >= 6) && !mutation.isPending;

  return (
    <main className="grid min-h-screen place-items-center bg-bg p-6 text-text">
      <section className="panel w-full max-w-[420px]">
        <div className="section-head">
          <div className="flex min-w-0 items-center gap-3">
            <div className="grid h-8 w-8 shrink-0 place-items-center rounded-md border border-border-strong bg-surface-2">
              <BrandLogo className="h-5 w-5 text-accent" />
            </div>
            <div className="min-w-0">
              <BrandWordmark />
              <h1 className="text-[20px] font-medium">{mode === "bootstrap" ? "Create first admin" : "Admin login"}</h1>
            </div>
          </div>
        </div>
        <div className={`mt-4 rounded-md border px-3 py-2 text-sm ${healthTone === "danger" ? "border-danger/40 text-danger" : healthTone === "success" ? "border-success/40 text-success" : "border-border text-muted"}`}>
          {healthMessage}
        </div>
        <div className="mt-2 rounded-md border border-border px-3 py-2 text-sm text-muted">
          {authStateMessage}
        </div>
        <form className="mt-5 grid gap-3" onSubmit={submit}>
          <input autoComplete="username" className="input" placeholder="admin@example.com" value={email} onChange={(event) => setEmail(event.target.value)} type="email" />
          <input autoComplete={mode === "bootstrap" ? "new-password" : "current-password"} className="input" placeholder={mode === "bootstrap" ? "Create password" : "Password"} value={password} onChange={(event) => setPassword(event.target.value)} type="password" />
          {mfaRequired ? (
            <input autoComplete="one-time-code" className="input" inputMode="numeric" maxLength={6} placeholder="123456" value={totpCode} onChange={(event) => setTOTPCode(event.target.value)} />
          ) : null}
          <button className="button justify-center" disabled={!canSubmit} type="submit">
            {mode === "bootstrap" ? "Create admin" : mfaRequired ? "Verify code" : "Login"}
          </button>
          {showBootstrapToggle ? (
            <button className="button secondary justify-center" onClick={() => {
              setMode(mode === "bootstrap" ? "login" : "bootstrap");
              setMFARequired(false);
              setTOTPCode("");
            }} type="button">
              {mode === "bootstrap" ? "Back to login" : "Create first admin"}
            </button>
          ) : null}
          <button className="button secondary justify-center" disabled={ssoMutation.isPending} onClick={() => ssoMutation.mutate()} type="button">
            Continue with SSO
          </button>
          {mfaRequired ? <p className="text-sm text-muted">Enter the six-digit code from your authenticator app.</p> : null}
          {authError ? <p className="text-sm text-danger">{authError}</p> : null}
          {ssoError ? <p className="text-sm text-danger">{ssoError}</p> : null}
        </form>
      </section>
    </main>
  );
}

function authErrorMessage(error: Error, apiBase: string, bootstrapped?: boolean) {
  if (error.message === "Failed to fetch" || error.message === "NetworkError when attempting to fetch resource.") {
    return `Cannot reach the Management API at ${apiBase}. Start the control plane there or set VITE_API_BASE_URL to the running API.`;
  }
  if (error.message.includes("404")) {
    return `The server at ${apiBase} is not exposing the supadupa auth API. Check SUPADUPA_ADDR or VITE_API_BASE_URL.`;
  }
  if (error.message.includes("admin user already exists")) {
    return "An admin already exists. Switch to Admin login.";
  }
  if (error.message.includes("invalid credentials")) {
    if (bootstrapped === false) {
      return "Invalid credentials. Create the first admin account, then use Admin login.";
    }
    return "Invalid credentials for this admin account.";
  }
  if (error.message.includes("platform sso is disabled")) {
    return "Platform SSO is not enabled. Configure SAML SSO in Settings first.";
  }
  if (error.message.includes("missing a login URL")) {
    return "Platform SSO is enabled but no IdP login URL is configured.";
  }
  return error.message;
}

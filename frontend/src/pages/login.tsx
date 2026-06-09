import { FormEvent, useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  bootstrapAdmin,
  getApiHealth,
  getAuthState,
  login,
  startPlatformSSO,
} from "../api";
import { BrandLogo } from "../components/brand-logo";
import { BrandWordmark } from "../components/brand-wordmark";
import { AppPanel } from "../components/app/app-panel";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { useAuthSession } from "../lib/auth-session";

export function LoginPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const setAuthenticated = useAuthSession((state) => state.setAuthenticated);
  const [mode, setMode] = useState<"bootstrap" | "login">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [totpCode, setTOTPCode] = useState("");
  const [mfaRequired, setMFARequired] = useState(false);
  const health = useQuery({
    queryKey: ["api-health"],
    queryFn: getApiHealth,
    retry: 1,
    refetchInterval: 15_000,
  });
  const authState = useQuery({
    queryKey: ["auth-state"],
    queryFn: ({ signal }) => getAuthState({ signal }),
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
    onSuccess: async (payload) => {
      if (payload.mfa_required) {
        setMFARequired(true);
        return;
      }
      setAuthenticated(payload.user);
      await queryClient.cancelQueries({ queryKey: ["auth-state"] });
      queryClient.setQueryData(["auth-state"], (current: unknown) => ({
        ...(typeof current === "object" && current !== null ? current : {}),
        authenticated: true,
        bootstrapped: true,
        user: payload.user,
      }));
      await queryClient.invalidateQueries({ queryKey: ["auth-state"] });
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

  // Both queries hit the same control plane, so a failure of either is one
  // connectivity problem, not two. Collapse to a single banner.
  const connectivityError = health.isError || authState.isError;
  const connectivityMessage = connectivityError
    ? "Cannot reach the Management API. Start the control plane or check the deployment configuration."
    : "";
  const noticeMessage = !connectivityError && !authState.isLoading && authState.data?.bootstrapped === false
    ? "No admin detected. Bootstrap is available below for first-run installs."
    : "";
  const authError = mutation.error ? authErrorMessage(mutation.error, authState.data?.bootstrapped) : "";
  const ssoError = ssoMutation.error ? authErrorMessage(ssoMutation.error, authState.data?.bootstrapped) : "";
  const showBootstrapToggle = mode === "bootstrap" || authState.data?.bootstrapped === false;
  // Only offer SSO when the control plane reports it enabled (safe pre-auth
  // signal from /v1/auth/state), and never during first-run bootstrap — there is
  // no IdP to fall back to before an admin exists.
  const showSSO = mode !== "bootstrap" && authState.data?.bootstrapped === true && authState.data?.sso_enabled === true;
  const canSubmit = email.trim().length > 0 && password.length > 0 && (!mfaRequired || totpCode.length >= 6) && !mutation.isPending;

  return (
    <main className="grid min-h-screen place-items-center bg-bg p-6 text-text">
      <AppPanel className="w-full max-w-[420px]">
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
        {connectivityMessage ? (
          <div className="mt-4 rounded-md border border-danger/40 px-3 py-2 text-sm text-danger">
            {connectivityMessage}
          </div>
        ) : null}
        {noticeMessage ? (
          <div className="mt-4 rounded-md border border-border px-3 py-2 text-sm text-muted">
            {noticeMessage}
          </div>
        ) : null}
        <form className="mt-5 grid gap-3" onSubmit={submit}>
          <label className="grid gap-1">
            <span className="sr-only">Email</span>
            <Input autoComplete="username" placeholder="admin@example.com" value={email} onChange={(event) => setEmail(event.target.value)} type="email" />
          </label>
          <label className="grid gap-1">
            <span className="sr-only">Password</span>
            <Input autoComplete={mode === "bootstrap" ? "new-password" : "current-password"} placeholder={mode === "bootstrap" ? "Create password" : "Password"} value={password} onChange={(event) => setPassword(event.target.value)} type="password" />
          </label>
          {mfaRequired ? (
            <label className="grid gap-1">
              <span className="sr-only">Authenticator code</span>
              <Input autoComplete="one-time-code" inputMode="numeric" maxLength={6} placeholder="123456" value={totpCode} onChange={(event) => setTOTPCode(event.target.value)} />
            </label>
          ) : null}
          <Button className="justify-center" disabled={!canSubmit} type="submit">
            {mode === "bootstrap" ? "Create first admin" : mfaRequired ? "Verify code" : "Login"}
          </Button>
          {showBootstrapToggle ? (
            <Button className="justify-center" variant="secondary" onClick={() => {
              setMode(mode === "bootstrap" ? "login" : "bootstrap");
              setMFARequired(false);
              setTOTPCode("");
            }} type="button">
              {mode === "bootstrap" ? "Back to login" : "Create first admin"}
            </Button>
          ) : null}
          {showSSO ? (
            <Button className="justify-center" variant="secondary" disabled={ssoMutation.isPending} onClick={() => ssoMutation.mutate()} type="button">
              Continue with SSO
            </Button>
          ) : null}
          {mfaRequired ? <p className="text-sm text-muted">Enter the six-digit code from your authenticator app.</p> : null}
          {authError ? <p className="text-sm text-danger">{authError}</p> : null}
          {ssoError ? <p className="text-sm text-danger">{ssoError}</p> : null}
        </form>
      </AppPanel>
    </main>
  );
}

function authErrorMessage(error: Error, bootstrapped?: boolean) {
  if (error.message === "Failed to fetch" || error.message === "NetworkError when attempting to fetch resource.") {
    return "Cannot reach the Management API. Start the control plane or check VITE_API_BASE_URL.";
  }
  if (error.message.includes("404")) {
    return "The configured server is not exposing the supadupa auth API. Check SUPADUPA_ADDR or VITE_API_BASE_URL.";
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

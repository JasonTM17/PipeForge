import { FormEvent, useCallback, useEffect, useRef, useState } from "react";
import {
  APIError,
  Artifact,
  Dataset,
  Job,
  authenticate,
  isAbortError,
  loadArtifacts,
  loadDatasets,
  loadJobs,
} from "./api";
import ConsoleView from "./console-view";

const tokenKey = "pipeforge.accessToken";
const selectedJobParameter = "job";

function initialSelectedJob() {
  return new URLSearchParams(window.location.search).get(selectedJobParameter) ?? "";
}

function errorMessage(reason: unknown, fallback: string) {
  return reason instanceof Error ? reason.message : fallback;
}

export default function App() {
  const [token, setToken] = useState(() => sessionStorage.getItem(tokenKey) ?? "");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [datasets, setDatasets] = useState<Dataset[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [selectedJob, setSelectedJob] = useState(initialSelectedJob);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  const [loading, setLoading] = useState(false);
  const [signingIn, setSigningIn] = useState(false);
  const [jobsLoaded, setJobsLoaded] = useState(false);
  const [refreshVersion, setRefreshVersion] = useState(0);
  const sessionGeneration = useRef(0);

  const clearSession = useCallback((message = "") => {
    sessionGeneration.current += 1;
    sessionStorage.removeItem(tokenKey);
    setToken("");
    setDatasets([]);
    setJobs([]);
    setArtifacts([]);
    setSelectedJob("");
    setJobsLoaded(false);
    setLoading(false);
    setError("");
    setStatus(message);
    const url = new URL(window.location.href);
    url.searchParams.delete(selectedJobParameter);
    window.history.replaceState({}, "", url);
  }, []);

  const handleRequestError = useCallback(
    (reason: unknown, generation: number, fallback: string) => {
      if (isAbortError(reason) || generation !== sessionGeneration.current) return;
      if (reason instanceof APIError && reason.status === 401) {
        clearSession("Your session expired. Sign in again.");
        return;
      }
      setError(errorMessage(reason, fallback));
    },
    [clearSession],
  );

  useEffect(() => {
    if (!token) return undefined;
    const controller = new AbortController();
    const generation = sessionGeneration.current;
    setLoading(true);
    setError("");
    setArtifacts([]);
    setJobsLoaded(false);
    void Promise.all([loadDatasets(token, controller.signal), loadJobs(token, controller.signal)])
      .then(([datasetPage, jobPage]) => {
        if (generation !== sessionGeneration.current) return;
        setDatasets(datasetPage.items);
        setJobs(jobPage.items);
        setJobsLoaded(true);
      })
      .catch((reason: unknown) => handleRequestError(reason, generation, "Unable to load the console"))
      .finally(() => {
        if (!controller.signal.aborted && generation === sessionGeneration.current) setLoading(false);
      });
    return () => controller.abort();
  }, [handleRequestError, refreshVersion, token]);

  useEffect(() => {
    if (!token || !jobsLoaded) return undefined;
    if (!selectedJob || !jobs.some((job) => job.id === selectedJob)) {
      if (selectedJob) setSelectedJob("");
      setArtifacts([]);
      return undefined;
    }
    const controller = new AbortController();
    const generation = sessionGeneration.current;
    setArtifacts([]);
    setError("");
    void loadArtifacts(token, selectedJob, controller.signal)
      .then((page) => {
        if (!controller.signal.aborted && generation === sessionGeneration.current) setArtifacts(page.items);
      })
      .catch((reason: unknown) => handleRequestError(reason, generation, "Unable to load artifacts"));
    return () => controller.abort();
  }, [handleRequestError, jobs, jobsLoaded, selectedJob, token]);

  useEffect(() => {
    const url = new URL(window.location.href);
    if (selectedJob) {
      url.searchParams.set(selectedJobParameter, selectedJob);
    } else {
      url.searchParams.delete(selectedJobParameter);
    }
    window.history.replaceState({}, "", url);
  }, [selectedJob]);

  async function signIn(event: FormEvent) {
    event.preventDefault();
    setSigningIn(true);
    setError("");
    setStatus("");
    try {
      const response = await authenticate(email, password);
      sessionGeneration.current += 1;
      sessionStorage.setItem(tokenKey, response.accessToken);
      setPassword("");
      setToken(response.accessToken);
      setStatus("Signed in.");
    } catch (reason) {
      if (!isAbortError(reason)) setError(errorMessage(reason, "Authentication failed"));
    } finally {
      setSigningIn(false);
    }
  }

  function signOut() {
    clearSession("Signed out.");
  }

  function selectJob(jobId: string) {
    setSelectedJob(jobId);
  }

  if (!token) {
    return (
      <main className="auth-shell" id="main-content">
        <form className="auth-card" onSubmit={signIn} aria-busy={signingIn}>
          <span className="eyebrow">PIPEFORGE / LOCAL CONSOLE</span>
          <h1>See the pipeline clearly.</h1>
          <p>Sign in with a local PipeForge account to inspect real owner-scoped data.</p>
          <label htmlFor="email">Email</label>
          <input id="email" value={email} onChange={(event) => setEmail(event.target.value)} type="email" autoComplete="email" required />
          <label htmlFor="password">Password</label>
          <input id="password" value={password} onChange={(event) => setPassword(event.target.value)} type="password" autoComplete="current-password" required />
          {status && <p className="status" role="status">{status}</p>}
          {error && <p className="error" role="alert">{error}</p>}
          <button type="submit" disabled={signingIn}>{signingIn ? "Signing in…" : "Open console"}</button>
        </form>
      </main>
    );
  }

  return (
    <ConsoleView
      artifacts={artifacts}
      datasets={datasets}
      error={error}
      jobs={jobs}
      jobsLoaded={jobsLoaded}
      loading={loading}
      onRefresh={() => setRefreshVersion((value) => value + 1)}
      onSelectJob={selectJob}
      onSignOut={signOut}
      selectedJob={selectedJob}
      status={status}
    />
  );
}

import { FormEvent, useEffect, useState } from "react";
import { Artifact, Dataset, Job, authenticate, loadArtifacts, loadDatasets, loadJobs } from "./api";

const tokenKey = "pipeforge.accessToken";

export default function App() {
  const [token, setToken] = useState(() => sessionStorage.getItem(tokenKey) ?? "");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [datasets, setDatasets] = useState<Dataset[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [selectedJob, setSelectedJob] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!token) return;
    void refresh(token);
  }, [token]);

  async function refresh(accessToken: string) {
    setLoading(true);
    setError("");
    try {
      const [datasetPage, jobPage] = await Promise.all([loadDatasets(accessToken), loadJobs(accessToken)]);
      setDatasets(datasetPage.items);
      setJobs(jobPage.items);
      if (selectedJob && jobPage.items.some((job) => job.id === selectedJob)) {
        setArtifacts((await loadArtifacts(accessToken, selectedJob)).items);
      }
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Unable to load the console");
    } finally {
      setLoading(false);
    }
  }

  async function signIn(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      const response = await authenticate(email, password);
      sessionStorage.setItem(tokenKey, response.accessToken);
      setToken(response.accessToken);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Authentication failed");
    }
  }

  function signOut() {
    sessionStorage.removeItem(tokenKey);
    setToken("");
    setDatasets([]);
    setJobs([]);
    setArtifacts([]);
  }

  async function selectJob(jobId: string) {
    setSelectedJob(jobId);
    try {
      setArtifacts((await loadArtifacts(token, jobId)).items);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Unable to load artifacts");
    }
  }

  if (!token) {
    return <main className="auth-shell"><form className="auth-card" onSubmit={signIn}><span className="eyebrow">PIPEFORGE / LOCAL CONSOLE</span><h1>See the pipeline clearly.</h1><p>Sign in with a local PipeForge account to inspect real owner-scoped data.</p><label>Email<input value={email} onChange={(event) => setEmail(event.target.value)} type="email" required /></label><label>Password<input value={password} onChange={(event) => setPassword(event.target.value)} type="password" required /></label>{error && <p className="error">{error}</p>}<button type="submit">Open console</button></form></main>;
  }

  const succeeded = jobs.filter((job) => job.state === "SUCCEEDED").length;
  return <main className="app-shell"><header><div><span className="eyebrow">PIPEFORGE / OPERATOR VIEW</span><h1>Runtime signal, without the noise.</h1></div><div className="header-actions"><button className="secondary" onClick={() => void refresh(token)} disabled={loading}>{loading ? "Refreshing…" : "Refresh"}</button><button className="secondary" onClick={signOut}>Sign out</button></div></header>{error && <div className="error banner">{error}</div>}<section className="kpis"><div><span>Datasets</span><strong>{datasets.length}</strong></div><div><span>Jobs observed</span><strong>{jobs.length}</strong></div><div><span>Succeeded</span><strong>{succeeded}</strong></div><div><span>Selected artifacts</span><strong>{artifacts.length}</strong></div></section><section className="grid"><article><div className="section-title"><h2>Recent jobs</h2><span>live API data</span></div>{jobs.length === 0 ? <p className="muted">No jobs are visible for this account.</p> : <div className="table">{jobs.map((job) => <button className={`row ${selectedJob === job.id ? "selected" : ""}`} key={job.id} onClick={() => void selectJob(job.id)}><span className="mono">{job.id.slice(0, 8)}</span><span className={`state state-${job.state.toLowerCase()}`}>{job.state}</span><span>{new Date(job.updatedAt).toLocaleString()}</span></button>)}</div>}</article><article><div className="section-title"><h2>Datasets</h2><span>owner scoped</span></div>{datasets.length === 0 ? <p className="muted">No datasets are visible for this account.</p> : <div className="table">{datasets.map((dataset) => <div className="row" key={dataset.id}><span>{dataset.name}</span><span className="state">{dataset.state}</span><span>{new Date(dataset.updatedAt).toLocaleDateString()}</span></div>)}</div>}</article><article className="wide"><div className="section-title"><h2>Canonical artifacts</h2><span>{selectedJob ? `job ${selectedJob.slice(0, 8)}` : "select a job"}</span></div>{artifacts.length === 0 ? <p className="muted">Select a job to inspect its canonical artifacts.</p> : <div className="artifact-list">{artifacts.map((artifact) => <div className="artifact" key={artifact.id}><span className="artifact-mark">●</span><div><strong>{artifact.kind}</strong><small>{artifact.state} · {artifact.sizeBytes.toLocaleString()} bytes</small></div></div>)}</div>}</article></section></main>;
}

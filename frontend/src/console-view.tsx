import { Artifact, Dataset, Job } from "./api";

type ConsoleViewProps = {
  artifacts: Artifact[];
  datasets: Dataset[];
  error: string;
  jobs: Job[];
  jobsLoaded: boolean;
  loading: boolean;
  onRefresh: () => void;
  onSelectJob: (jobId: string) => void;
  onSignOut: () => void;
  selectedJob: string;
  status: string;
};

export default function ConsoleView({
  artifacts,
  datasets,
  error,
  jobs,
  jobsLoaded,
  loading,
  onRefresh,
  onSelectJob,
  onSignOut,
  selectedJob,
  status,
}: ConsoleViewProps) {
  const succeeded = jobs.filter((job) => job.state === "SUCCEEDED").length;
  return (
    <>
      <a className="skip-link" href="#main-content">Skip to main content</a>
      <main className="app-shell" id="main-content" aria-busy={loading}>
        <header>
          <div><span className="eyebrow">PIPEFORGE / OPERATOR VIEW</span><h1>Runtime signal, without the noise.</h1></div>
          <div className="header-actions">
            <button className="secondary" onClick={onRefresh} disabled={loading}>{loading ? "Refreshing…" : "Refresh"}</button>
            <button className="secondary" onClick={onSignOut}>Sign out</button>
          </div>
        </header>
        {status && <p className="status" role="status">{status}</p>}
        {error && <div className="error banner" role="alert">{error}</div>}
        <section className="kpis" aria-label="Pipeline summary">
          <div><span>Datasets</span><strong>{datasets.length}</strong></div>
          <div><span>Jobs observed</span><strong>{jobs.length}</strong></div>
          <div><span>Succeeded</span><strong>{succeeded}</strong></div>
          <div><span>Selected artifacts</span><strong>{artifacts.length}</strong></div>
        </section>
        <section className="grid">
          <article>
            <div className="section-title"><h2>Recent jobs</h2><span>live API data</span></div>
            {jobsLoaded && jobs.length === 0 ? <p className="muted">No jobs are visible for this account.</p> : (
              <div className="table">{jobs.map((job) => (
                <button className={`row ${selectedJob === job.id ? "selected" : ""}`} aria-pressed={selectedJob === job.id} key={job.id} onClick={() => onSelectJob(job.id)}>
                  <span className="mono">{job.id.slice(0, 8)}</span><span className={`state state-${job.state.toLowerCase()}`}>{job.state}</span><span>{new Date(job.updatedAt).toLocaleString()}</span>
                </button>
              ))}</div>
            )}
          </article>
          <article>
            <div className="section-title"><h2>Datasets</h2><span>owner scoped</span></div>
            {!loading && datasets.length === 0 ? <p className="muted">No datasets are visible for this account.</p> : (
              <div className="table">{datasets.map((dataset) => <div className="row" key={dataset.id}><span>{dataset.name}</span><span className="state">{dataset.state}</span><span>{new Date(dataset.updatedAt).toLocaleDateString()}</span></div>)}</div>
            )}
          </article>
          <article className="wide">
            <div className="section-title"><h2>Canonical artifacts</h2><span>{selectedJob ? `job ${selectedJob.slice(0, 8)}` : "select a job"}</span></div>
            {artifacts.length === 0 ? <p className="muted">{selectedJob ? "No canonical artifacts are available for this job." : "Select a job to inspect its canonical artifacts."}</p> : (
              <div className="artifact-list">{artifacts.map((artifact) => <div className="artifact" key={artifact.id}><span className="artifact-mark" aria-hidden="true">●</span><div><strong>{artifact.kind}</strong><small>{artifact.state} · {artifact.sizeBytes.toLocaleString()} bytes</small></div></div>)}</div>
            )}
          </article>
        </section>
      </main>
    </>
  );
}

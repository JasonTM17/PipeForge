import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "../../src/App";
import { APIError, Artifact, Job, authenticate, loadArtifacts, loadDatasets, loadJobs } from "../../src/api";

vi.mock("../../src/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("../../src/api")>();
  return {
    ...original,
    authenticate: vi.fn(),
    loadArtifacts: vi.fn(),
    loadDatasets: vi.fn(),
    loadJobs: vi.fn(),
  };
});

const jobs: Job[] = [
  { id: "job-one-1234", datasetVersionId: "version-1", state: "SUCCEEDED", createdAt: "2026-08-01T10:00:00Z", updatedAt: "2026-08-01T10:01:00Z" },
  { id: "job-two-5678", datasetVersionId: "version-2", state: "RUNNING", createdAt: "2026-08-01T11:00:00Z", updatedAt: "2026-08-01T11:01:00Z" },
];

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(authenticate).mockResolvedValue({ accessToken: "token-1" });
  vi.mocked(loadDatasets).mockResolvedValue({ items: [] });
  vi.mocked(loadJobs).mockResolvedValue({ items: jobs });
  vi.mocked(loadArtifacts).mockResolvedValue({ items: [] });
});

describe("PipeForge console", () => {
  it("provides an accessible sign-in form", () => {
    render(<App />);
    expect(screen.getByRole("heading", { name: "See the pipeline clearly." })).toBeVisible();
    expect(screen.getByLabelText("Email")).toHaveAttribute("autocomplete", "email");
    expect(screen.getByLabelText("Password")).toHaveAttribute("autocomplete", "current-password");
    expect(screen.getByRole("button", { name: "Open console" })).toBeEnabled();
  });

  it("signs in, refreshes, and signs out without retaining owner data", async () => {
    const user = userEvent.setup();
    render(<App />);
    await user.type(screen.getByLabelText("Email"), "owner@example.com");
    await user.type(screen.getByLabelText("Password"), "secret");
    await user.click(screen.getByRole("button", { name: "Open console" }));

    expect(await screen.findByRole("heading", { name: "Runtime signal, without the noise." })).toBeVisible();
    expect(authenticate).toHaveBeenCalledWith("owner@example.com", "secret");
    await user.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => expect(loadJobs).toHaveBeenCalledTimes(2));
    await user.click(screen.getByRole("button", { name: "Sign out" }));
    expect(screen.getByText("Signed out.")).toHaveAttribute("role", "status");
    expect(sessionStorage.getItem("pipeforge.accessToken")).toBeNull();
  });

  it("ignores a superseded artifact response", async () => {
    sessionStorage.setItem("pipeforge.accessToken", "token-1");
    const first = deferred<{ items: Artifact[] }>();
    const second = deferred<{ items: Artifact[] }>();
    vi.mocked(loadArtifacts).mockImplementation((_token, jobId) => jobId === jobs[0].id ? first.promise : second.promise);
    const user = userEvent.setup();
    render(<App />);

    await user.click(await screen.findByRole("button", { name: /job-one/i }));
    await user.click(screen.getByRole("button", { name: /job-two/i }));
    second.resolve({ items: [{ id: "artifact-new", jobId: jobs[1].id, kind: "CANONICAL_JSON", sizeBytes: 42, state: "READY" }] });
    expect(await screen.findByText("CANONICAL_JSON")).toBeVisible();
    first.resolve({ items: [{ id: "artifact-old", jobId: jobs[0].id, kind: "STALE", sizeBytes: 10, state: "READY" }] });
    await waitFor(() => expect(screen.queryByText("STALE")).not.toBeInTheDocument());
    expect(window.location.search).toBe(`?job=${jobs[1].id}`);
  });

  it("clears an expired session on a 401 response", async () => {
    sessionStorage.setItem("pipeforge.accessToken", "expired");
    vi.mocked(loadDatasets).mockRejectedValue(new APIError("Unauthorized", 401, "UNAUTHORIZED"));
    render(<App />);

    expect(await screen.findByText("Your session expired. Sign in again.")).toHaveAttribute("role", "status");
    expect(sessionStorage.getItem("pipeforge.accessToken")).toBeNull();
    expect(screen.getByRole("heading", { name: "See the pipeline clearly." })).toBeVisible();
  });

  it("removes a stale deep-linked job after the job list loads", async () => {
    sessionStorage.setItem("pipeforge.accessToken", "token-1");
    window.history.replaceState({}, "", "/?job=missing-job");
    render(<App />);

    await screen.findByRole("button", { name: /job-one/i });
    await waitFor(() => expect(window.location.search).toBe(""));
    expect(loadArtifacts).not.toHaveBeenCalled();
  });
});

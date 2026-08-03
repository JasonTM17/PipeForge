export type TokenResponse = { accessToken: string };
export type Dataset = { id: string; name: string; state: string; updatedAt: string };
export type Job = { id: string; datasetVersionId: string; state: string; createdAt: string; updatedAt: string };
export type Artifact = { id: string; jobId: string; kind: string; sizeBytes: number; state: string };

const requestTimeoutMs = 15_000;

export class RequestTimeoutError extends Error {
  constructor() {
    super("The request timed out. Try again.");
    this.name = "RequestTimeoutError";
  }
}

export class APIError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
  ) {
    super(message);
    this.name = "APIError";
  }
}

type Problem = { message?: string; code?: string };

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const controller = new AbortController();
  let timedOut = false;
  const timeout = window.setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, requestTimeoutMs);
  const abortFromCaller = () => controller.abort(init.signal?.reason);
  if (init.signal?.aborted) {
    abortFromCaller();
  } else {
    init.signal?.addEventListener("abort", abortFromCaller, { once: true });
  }
  try {
    const response = await fetch(path, {
      ...init,
      signal: controller.signal,
      headers: { "Content-Type": "application/json", ...init.headers },
    });
    if (!response.ok) {
      const problem = (await response.json().catch(() => ({}))) as Problem;
      throw new APIError(problem.message ?? `Request failed (${response.status})`, response.status, problem.code);
    }
    return response.status === 204 ? (undefined as T) : response.json();
  } catch (reason) {
    if (timedOut) {
      throw new RequestTimeoutError();
    }
    throw reason;
  } finally {
    window.clearTimeout(timeout);
    init.signal?.removeEventListener("abort", abortFromCaller);
  }
}

export function isAbortError(reason: unknown): boolean {
  return reason instanceof DOMException && reason.name === "AbortError";
}

export function authenticate(email: string, password: string, signal?: AbortSignal) {
  return request<TokenResponse>("/v1/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password }),
    signal,
  });
}

export function loadDatasets(token: string, signal?: AbortSignal) {
  return request<{ items: Dataset[] }>("/v1/datasets?page=1&pageSize=20", {
    headers: { Authorization: `Bearer ${token}` },
    signal,
  });
}

export function loadJobs(token: string, signal?: AbortSignal) {
  return request<{ items: Job[] }>("/api/v1/jobs?page=1&pageSize=20", {
    headers: { Authorization: `Bearer ${token}` },
    signal,
  });
}

export function loadArtifacts(token: string, jobId: string, signal?: AbortSignal) {
  return request<{ items: Artifact[] }>(`/api/v1/jobs/${jobId}/artifacts?page=1&pageSize=20`, {
    headers: { Authorization: `Bearer ${token}` },
    signal,
  });
}

export type TokenResponse = { accessToken: string };
export type Dataset = { id: string; name: string; state: string; updatedAt: string };
export type Job = { id: string; datasetVersionId: string; state: string; createdAt: string; updatedAt: string };
export type Artifact = { id: string; jobId: string; kind: string; sizeBytes: number; state: string };

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: { "Content-Type": "application/json", ...init.headers },
  });
  if (!response.ok) {
    const problem = await response.json().catch(() => ({}));
    throw new Error(problem.message ?? `Request failed (${response.status})`);
  }
  return response.status === 204 ? (undefined as T) : response.json();
}

export function authenticate(email: string, password: string) {
  return request<TokenResponse>("/v1/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  });
}

export function loadDatasets(token: string) {
  return request<{ items: Dataset[] }>("/v1/datasets?page=1&pageSize=20", { headers: { Authorization: `Bearer ${token}` } });
}

export function loadJobs(token: string) {
  return request<{ items: Job[] }>("/api/v1/jobs?page=1&pageSize=20", { headers: { Authorization: `Bearer ${token}` } });
}

export function loadArtifacts(token: string, jobId: string) {
  return request<{ items: Artifact[] }>(`/api/v1/jobs/${jobId}/artifacts?page=1&pageSize=20`, { headers: { Authorization: `Bearer ${token}` } });
}

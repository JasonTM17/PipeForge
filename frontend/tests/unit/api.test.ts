import { afterEach, describe, expect, it, vi } from "vitest";
import { APIError, RequestTimeoutError, authenticate, isAbortError, loadArtifacts, loadDatasets, loadJobs } from "../../src/api";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("API client", () => {
  it("sends credentials and parses a token", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ accessToken: "token-1" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    await expect(authenticate("owner@example.com", "secret")).resolves.toEqual({ accessToken: "token-1" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/v1/auth/login",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ email: "owner@example.com", password: "secret" }) }),
    );
  });

  it("preserves structured API failures", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ message: "Session expired", code: "UNAUTHORIZED" }), {
        status: 401,
        headers: { "Content-Type": "application/problem+json" },
      }),
    );

    const failure = await loadDatasets("expired").catch((reason: unknown) => reason);
    expect(failure).toBeInstanceOf(APIError);
    expect(failure).toMatchObject({ message: "Session expired", status: 401, code: "UNAUTHORIZED" });
  });

  it("propagates caller cancellation", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation((_input, init) => new Promise((_resolve, reject) => {
      init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true });
    }));
    const controller = new AbortController();
    const request = loadDatasets("token", controller.signal);
    controller.abort();

    const failure = await request.catch((reason: unknown) => reason);
    expect(isAbortError(failure)).toBe(true);
  });

  it("reports its bounded request timeout", async () => {
    vi.useFakeTimers();
    vi.spyOn(globalThis, "fetch").mockImplementation((_input, init) => new Promise((_resolve, reject) => {
      init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true });
    }));
    const request = loadDatasets("token");
    const assertion = expect(request).rejects.toBeInstanceOf(RequestTimeoutError);
    await vi.advanceTimersByTimeAsync(15_000);

    await assertion;
  });

  it("sends bearer credentials to job and artifact endpoints", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async () =>
      new Response(JSON.stringify({ items: [] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    await loadJobs("token-1");
    await loadArtifacts("token-1", "job-1");
    expect(fetchMock).toHaveBeenNthCalledWith(1, "/api/v1/jobs?page=1&pageSize=20", expect.objectContaining({ headers: expect.objectContaining({ Authorization: "Bearer token-1" }) }));
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/api/v1/jobs/job-1/artifacts?page=1&pageSize=20", expect.objectContaining({ headers: expect.objectContaining({ Authorization: "Bearer token-1" }) }));
  });
});

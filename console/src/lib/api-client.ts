export const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "/api/v1";

export class ApiError extends Error {
	status: number;

	constructor(status: number, message: string) {
		super(message);
		this.status = status;
	}
}

interface RequestOptions extends Omit<RequestInit, "body"> {
	body?: unknown;
	/** Return the raw response envelope instead of unwrapping `data`. */
	raw?: boolean;
}

/**
 * Fetch wrapper for the console API. Sends cookies on every request
 * (`credentials: "include"`) so the backend's httpOnly auth cookie is
 * attached automatically — the frontend never reads or stores the token.
 * Responses are wrapped as `{ data: ... }`; unwrapped to `data` by default,
 * pass `raw: true` to keep sibling fields like `pagination`.
 */
export async function apiFetch<T>(
	path: string,
	options: RequestOptions = {},
): Promise<T> {
	const { body, headers, raw, ...rest } = options;

	const response = await fetch(`${API_BASE_URL}${path}`, {
		...rest,
		credentials: "include",
		headers: {
			"Content-Type": "application/json",
			...headers,
		},
		body: body !== undefined ? JSON.stringify(body) : undefined,
	});

	if (!response.ok) {
		const message = await response
			.json()
			.then((data) => data?.message as string | undefined)
			.catch(() => undefined);
		throw new ApiError(
			response.status,
			message ?? `Request failed with status ${response.status}`,
		);
	}

	if (response.status === 204) return undefined as T;

	const json = (await response.json()) as { data?: T } | T;
	if (raw) return json as T;

	return json && typeof json === "object" && "data" in json
		? (json.data as T)
		: (json as T);
}

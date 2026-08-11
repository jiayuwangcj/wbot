import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError } from "../api/client";

export interface AsyncDataState<T> {
  data: T | null;
  loading: boolean;
  error: ApiError | Error | null;
  refresh: () => Promise<void>;
}

export function useAsyncData<T>(loader: () => Promise<T>, dependencies: readonly unknown[] = []): AsyncDataState<T> {
  const loaderRef = useRef(loader);
  loaderRef.current = loader;
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<ApiError | Error | null>(null);
  const mountedRef = useRef(true);
  const requestRef = useRef(0);
  useEffect(() => () => {
    mountedRef.current = false;
  }, []);
  const refresh = useCallback(async (): Promise<void> => {
    const request = requestRef.current + 1;
    requestRef.current = request;
    if (mountedRef.current) {
      setLoading(true);
      setError(null);
    }
    try {
      const nextData = await loaderRef.current();
      if (mountedRef.current && request === requestRef.current) setData(nextData);
    } catch (caught: unknown) {
      const nextError = caught instanceof Error ? caught : new Error("unexpected server response");
      if (mountedRef.current && request === requestRef.current) setError(nextError);
    } finally {
      if (mountedRef.current && request === requestRef.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh, ...dependencies]);

  return { data, loading, error, refresh };
}

import { useState, useMemo, useCallback } from "react";
import { useUiStore } from "@/stores/use-ui-store";

export interface PaginationState {
  page: number;
  pageSize: number;
  total: number;
  totalPages: number;
}

export interface UsePaginationOptions {
  defaultPageSize?: number;
}

export interface UsePaginationReturn<T> {
  pageItems: T[];
  pagination: PaginationState;
  setPage: (page: number) => void;
  setPageSize: (size: number) => void;
  resetPage: () => void;
}

export function usePagination<T>(
  items: T[],
  options: UsePaginationOptions = {},
): UsePaginationReturn<T> {
  const globalPageSize = useUiStore((s) => s.pageSize);
  const setGlobalPageSize = useUiStore((s) => s.setPageSize);

  const [page, setPageRaw] = useState(1);
  const [pageSize, setPageSizeRaw] = useState(options.defaultPageSize ?? globalPageSize);

  const total = items.length;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const safePage = Math.min(page, totalPages);

  const pageItems = useMemo(() => {
    const start = (safePage - 1) * pageSize;
    return items.slice(start, start + pageSize);
  }, [items, safePage, pageSize]);

  const setPage = useCallback(
    (p: number) => {
      setPageRaw(Math.max(1, Math.min(p, totalPages)));
    },
    [totalPages],
  );

  const setPageSize = useCallback((size: number) => {
    setPageSizeRaw(size);
    setPageRaw(1);
    setGlobalPageSize(size);
  }, [setGlobalPageSize]);

  const resetPage = useCallback(() => setPageRaw(1), []);

  return {
    pageItems,
    pagination: { page: safePage, pageSize, total, totalPages },
    setPage,
    setPageSize,
    resetPage,
  };
}

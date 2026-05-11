export interface PageParams {
  page: number;
  limit: number;
}

export interface PaginatedResponse<T> {
  items: T[];
  total_count: number;
}

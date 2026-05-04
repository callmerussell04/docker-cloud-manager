import axios from 'axios';

export interface ApiErrorResponse {
  error?: string;
  request_id?: string;
}

export interface ApiErrorMessage {
  message: string;
  requestId?: string;
}

export function getApiErrorMessage(error: unknown, fallback: string): ApiErrorMessage {
  if (axios.isAxiosError<ApiErrorResponse>(error)) {
    const responseError = error.response?.data?.error;
    const requestId = error.response?.data?.request_id;

    if (responseError) {
      return { message: responseError, requestId };
    }

    if (error.response) {
      return {
        message: fallback,
        requestId,
      };
    }

    if (error.code === 'ECONNABORTED') {
      return { message: 'Нет ответа от сервера' };
    }

    if (error.request) {
      return { message: 'Сервер недоступен' };
    }
  }

  return { message: fallback };
}

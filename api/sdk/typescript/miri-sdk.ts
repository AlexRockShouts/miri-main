/* eslint-disable */
/* tslint:disable */
// @ts-nocheck
/*
 * ---------------------------------------------------------------
 * ## THIS FILE WAS GENERATED VIA SWAGGER-TYPESCRIPT-API        ##
 * ##                                                           ##
 * ## AUTHOR: acacode                                           ##
 * ## SOURCE: https://github.com/acacode/swagger-typescript-api ##
 * ---------------------------------------------------------------
 */

export interface MiriSession {
  id?: string;
  client_id?: string;
  total_tokens?: number;
  messages?: MiriMessage[];
}

export interface MiriMessage {
  role?: string;
  content?: string;
}

export interface MiriUsage {
  prompt_tokens?: number;
  completion_tokens?: number;
  total_tokens?: number;
}

export interface MiriHumanInfo {
  id?: string;
  data?: Record<string, string>;
  notes?: string;
}

export interface MiriSkill {
  name?: string;
  description?: string;
  version?: string;
  tags?: string[];
}

export interface MiriConfig {
  storage_dir?: string;
  server?: {
    addr?: string;
    key?: string;
    admin_user?: string;
    admin_pass?: string;
  };
  models?: {
    mode?: string;
    providers?: Record<string, MiriProviderConfig>;
  };
  agents?: {
    debug?: boolean;
    defaults?: {
      model?: {
        primary?: string;
        fallbacks?: string[];
      };
    };
  };
  channels?: {
    whatsapp?: {
      enabled?: boolean;
      allowlist?: string[];
      blocklist?: string[];
    };
    irc?: {
      enabled?: boolean;
      host?: string;
      port?: number;
      tls?: boolean;
      nick?: string;
      user?: string;
      realname?: string;
      channels?: string[];
      nickserv?: {
        enabled?: boolean;
        password?: string;
      };
    };
  };
}

export interface MiriProviderConfig {
  baseUrl?: string;
  apiKey?: string;
  api?: string;
  models?: MiriModelConfig[];
}

export interface MiriModelConfig {
  id?: string;
  name?: string;
  contextWindow?: number;
  maxTokens?: number;
  reasoning?: boolean;
  input?: string[];
  cost?: {
    input?: number;
    output?: number;
    cacheRead?: number;
    cacheWrite?: number;
  };
}

import type {
  AxiosInstance,
  AxiosRequestConfig,
  AxiosResponse,
  HeadersDefaults,
  ResponseType,
} from "axios";
import axios from "axios";

export type QueryParamsType = Record<string | number, any>;

export interface FullRequestParams
  extends Omit<AxiosRequestConfig, "data" | "params" | "url" | "responseType"> {
  /** set parameter to `true` for call `securityWorker` for this request */
  secure?: boolean;
  /** request path */
  path: string;
  /** content type of request body */
  type?: ContentType;
  /** query params */
  query?: QueryParamsType;
  /** format of response (i.e. response.json() -> format: "json") */
  format?: ResponseType;
  /** request body */
  body?: unknown;
}

export type RequestParams = Omit<
  FullRequestParams,
  "body" | "method" | "query" | "path"
>;

export interface ApiConfig<SecurityDataType = unknown>
  extends Omit<AxiosRequestConfig, "data" | "cancelToken"> {
  securityWorker?: (
    securityData: SecurityDataType | null,
  ) => Promise<AxiosRequestConfig | void> | AxiosRequestConfig | void;
  secure?: boolean;
  format?: ResponseType;
}

export enum ContentType {
  Json = "application/json",
  JsonApi = "application/vnd.api+json",
  FormData = "multipart/form-data",
  UrlEncoded = "application/x-www-form-urlencoded",
  Text = "text/plain",
}

export class HttpClient<SecurityDataType = unknown> {
  public instance: AxiosInstance;
  private securityData: SecurityDataType | null = null;
  private securityWorker?: ApiConfig<SecurityDataType>["securityWorker"];
  private secure?: boolean;
  private format?: ResponseType;

  constructor({
    securityWorker,
    secure,
    format,
    ...axiosConfig
  }: ApiConfig<SecurityDataType> = {}) {
    this.instance = axios.create({
      ...axiosConfig,
      baseURL: axiosConfig.baseURL || "http://localhost:8080",
    });
    this.secure = secure;
    this.format = format;
    this.securityWorker = securityWorker;
  }

  public setSecurityData = (data: SecurityDataType | null) => {
    this.securityData = data;
  };

  protected mergeRequestParams(
    params1: AxiosRequestConfig,
    params2?: AxiosRequestConfig,
  ): AxiosRequestConfig {
    const method = params1.method || (params2 && params2.method);

    return {
      ...this.instance.defaults,
      ...params1,
      ...(params2 || {}),
      headers: {
        ...((method &&
          this.instance.defaults.headers[
            method.toLowerCase() as keyof HeadersDefaults
          ]) ||
          {}),
        ...(params1.headers || {}),
        ...((params2 && params2.headers) || {}),
      },
    };
  }

  protected stringifyFormItem(formItem: unknown) {
    if (typeof formItem === "object" && formItem !== null) {
      return JSON.stringify(formItem);
    } else {
      return `${formItem}`;
    }
  }

  protected createFormData(input: Record<string, unknown>): FormData {
    if (input instanceof FormData) {
      return input;
    }
    return Object.keys(input || {}).reduce((formData, key) => {
      const property = input[key];
      const propertyContent: any[] =
        property instanceof Array ? property : [property];

      for (const formItem of propertyContent) {
        const isFileType = formItem instanceof Blob || formItem instanceof File;
        formData.append(
          key,
          isFileType ? formItem : this.stringifyFormItem(formItem),
        );
      }

      return formData;
    }, new FormData());
  }

  public request = async <T = any, _E = any>({
    secure,
    path,
    type,
    query,
    format,
    body,
    ...params
  }: FullRequestParams): Promise<AxiosResponse<T>> => {
    const secureParams =
      ((typeof secure === "boolean" ? secure : this.secure) &&
        this.securityWorker &&
        (await this.securityWorker(this.securityData))) ||
      {};
    const requestParams = this.mergeRequestParams(params, secureParams);
    const responseFormat = format || this.format || undefined;

    if (
      type === ContentType.FormData &&
      body &&
      body !== null &&
      typeof body === "object"
    ) {
      body = this.createFormData(body as Record<string, unknown>);
    }

    if (
      type === ContentType.Text &&
      body &&
      body !== null &&
      typeof body !== "string"
    ) {
      body = JSON.stringify(body);
    }

    return this.instance.request({
      ...requestParams,
      headers: {
        ...(requestParams.headers || {}),
        ...(type ? { "Content-Type": type } : {}),
      },
      params: query,
      responseType: responseFormat,
      data: body,
      url: path,
    });
  };
}

/**
 * @title Miri Autonomous Agent API
 * @version 1.0.0
 * @baseUrl http://localhost:8080
 *
 * API for interacting with the Miri autonomous agent service.
 */
export class Api<
  SecurityDataType extends unknown,
> extends HttpClient<SecurityDataType> {
  api = {
    /**
     * No description
     *
     * @name V1PromptCreate
     * @summary Send a prompt to the agent
     * @request POST:/api/v1/prompt
     * @secure
     */
    v1PromptCreate: (
      data: {
        prompt?: string;
        session_id?: string;
        model?: string;
        temperature?: number;
        max_tokens?: number;
      },
      params: RequestParams = {},
    ) =>
      this.request<
        {
          response?: string;
        },
        any
      >({
        path: `/api/v1/prompt`,
        method: "POST",
        body: data,
        secure: true,
        type: ContentType.Json,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @name V1PromptStreamList
     * @summary Stream a prompt response via SSE
     * @request GET:/api/v1/prompt/stream
     * @secure
     */
    v1PromptStreamList: (
      query: {
        prompt: string;
        session_id?: string;
        model?: string;
      },
      params: RequestParams = {},
    ) =>
      this.request<string, any>({
        path: `/api/v1/prompt/stream`,
        method: "GET",
        query: query,
        secure: true,
        ...params,
      }),

    /**
     * No description
     *
     * @name V1InteractionCreate
     * @summary Manage sessions or check global status
     * @request POST:/api/v1/interaction
     * @secure
     */
    v1InteractionCreate: (
      data: {
        action?: "new" | "status";
        client_id?: string;
      },
      params: RequestParams = {},
    ) =>
      this.request<void, any>({
        path: `/api/v1/interaction`,
        method: "POST",
        body: data,
        secure: true,
        type: ContentType.Json,
        ...params,
      }),

    /**
     * No description
     *
     * @name AdminV1HealthList
     * @summary Check health of the admin API
     * @request GET:/api/admin/v1/health
     * @secure
     */
    adminV1HealthList: (params: RequestParams = {}) =>
      this.request<
        {
          status?: string;
          message?: string;
        },
        any
      >({
        path: `/api/admin/v1/health`,
        method: "GET",
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @name AdminV1ConfigList
     * @summary Get current configuration
     * @request GET:/api/admin/v1/config
     * @secure
     */
    adminV1ConfigList: (params: RequestParams = {}) =>
      this.request<MiriConfig, any>({
        path: `/api/admin/v1/config`,
        method: "GET",
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @name AdminV1ConfigCreate
     * @summary Update configuration
     * @request POST:/api/admin/v1/config
     * @secure
     */
    adminV1ConfigCreate: (data: MiriConfig, params: RequestParams = {}) =>
      this.request<void, any>({
        path: `/api/admin/v1/config`,
        method: "POST",
        body: data,
        secure: true,
        type: ContentType.Json,
        ...params,
      }),

    /**
     * No description
     *
     * @name AdminV1HumanList
     * @summary List all stored human information
     * @request GET:/api/admin/v1/human
     * @secure
     */
    adminV1HumanList: (params: RequestParams = {}) =>
      this.request<MiriHumanInfo[], any>({
        path: `/api/admin/v1/human`,
        method: "GET",
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @name AdminV1HumanCreate
     * @summary Store human information
     * @request POST:/api/admin/v1/human
     * @secure
     */
    adminV1HumanCreate: (data: MiriHumanInfo, params: RequestParams = {}) =>
      this.request<void, any>({
        path: `/api/admin/v1/human`,
        method: "POST",
        body: data,
        secure: true,
        type: ContentType.Json,
        ...params,
      }),

    /**
     * No description
     *
     * @name AdminV1SkillsList
     * @summary List all installed skills
     * @request GET:/api/admin/v1/skills
     * @secure
     */
    adminV1SkillsList: (params: RequestParams = {}) =>
      this.request<MiriSkill[], any>({
        path: `/api/admin/v1/skills`,
        method: "GET",
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @name AdminV1ChannelsCreate
     * @summary Perform actions on communication channels
     * @request POST:/api/admin/v1/channels
     * @secure
     */
    adminV1ChannelsCreate: (
      data: {
        channel?: string;
        action?: "status" | "enroll" | "send" | "devices" | "chat";
        device?: string;
        message?: string;
        prompt?: string;
      },
      params: RequestParams = {},
    ) =>
      this.request<void, any>({
        path: `/api/admin/v1/channels`,
        method: "POST",
        body: data,
        secure: true,
        type: ContentType.Json,
        ...params,
      }),

    /**
     * No description
     *
     * @name AdminV1SessionsList
     * @summary List active session IDs
     * @request GET:/api/admin/v1/sessions
     * @secure
     */
    adminV1SessionsList: (params: RequestParams = {}) =>
      this.request<string[], any>({
        path: `/api/admin/v1/sessions`,
        method: "GET",
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @name AdminV1SessionsDetail
     * @summary Get session details
     * @request GET:/api/admin/v1/sessions/{id}
     * @secure
     */
    adminV1SessionsDetail: (id: string, params: RequestParams = {}) =>
      this.request<MiriSession, any>({
        path: `/api/admin/v1/sessions/${id}`,
        method: "GET",
        secure: true,
        format: "json",
        ...params,
      }),

    /**
     * No description
     *
     * @name AdminV1SessionsHistoryList
     * @summary Get session message history
     * @request GET:/api/admin/v1/sessions/{id}/history
     * @secure
     */
    adminV1SessionsHistoryList: (id: string, params: RequestParams = {}) =>
      this.request<
        {
          messages?: MiriMessage[];
          total_tokens?: number;
        },
        any
      >({
        path: `/api/admin/v1/sessions/${id}/history`,
        method: "GET",
        secure: true,
        format: "json",
        ...params,
      }),
  };
  ws = {
    /**
     * No description
     *
     * @name GetWs
     * @summary WebSocket for interactive streaming
     * @request GET:/ws
     * @secure
     */
    getWs: (
      query?: {
        session_id?: string;
        client_id?: string;
        channel?: string;
        device?: string;
      },
      params: RequestParams = {},
    ) =>
      this.request<any, void>({
        path: `/ws`,
        method: "GET",
        query: query,
        secure: true,
        ...params,
      }),
  };
}

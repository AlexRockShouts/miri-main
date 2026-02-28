# DefaultApi

All URIs are relative to *http://localhost:8080*

|Method | HTTP request | Description|
|------------- | ------------- | -------------|
|[**apiAdminV1ChannelsPost**](#apiadminv1channelspost) | **POST** /api/admin/v1/channels | Perform actions on communication channels|
|[**apiAdminV1ConfigGet**](#apiadminv1configget) | **GET** /api/admin/v1/config | Get current configuration|
|[**apiAdminV1ConfigPost**](#apiadminv1configpost) | **POST** /api/admin/v1/config | Update configuration|
|[**apiAdminV1HealthGet**](#apiadminv1healthget) | **GET** /api/admin/v1/health | Check health of the admin API|
|[**apiAdminV1HumanGet**](#apiadminv1humanget) | **GET** /api/admin/v1/human | List all stored human information|
|[**apiAdminV1HumanPost**](#apiadminv1humanpost) | **POST** /api/admin/v1/human | Store human information|
|[**apiAdminV1SessionsGet**](#apiadminv1sessionsget) | **GET** /api/admin/v1/sessions | List active session IDs|
|[**apiAdminV1SessionsIdGet**](#apiadminv1sessionsidget) | **GET** /api/admin/v1/sessions/{id} | Get session details|
|[**apiAdminV1SessionsIdHistoryGet**](#apiadminv1sessionsidhistoryget) | **GET** /api/admin/v1/sessions/{id}/history | Get session message history|
|[**apiAdminV1SessionsIdSkillsGet**](#apiadminv1sessionsidskillsget) | **GET** /api/admin/v1/sessions/{id}/skills | Get loaded skills for a session|
|[**apiAdminV1SessionsIdStatsGet**](#apiadminv1sessionsidstatsget) | **GET** /api/admin/v1/sessions/{id}/stats | Get session token and cost statistics|
|[**apiAdminV1SkillsCommandsGet**](#apiadminv1skillscommandsget) | **GET** /api/admin/v1/skills/commands | List all available agent commands (tools)|
|[**apiAdminV1SkillsGet**](#apiadminv1skillsget) | **GET** /api/admin/v1/skills | List all installed skills|
|[**apiAdminV1SkillsNameDelete**](#apiadminv1skillsnamedelete) | **DELETE** /api/admin/v1/skills/{name} | Remove a skill|
|[**apiAdminV1SkillsNameGet**](#apiadminv1skillsnameget) | **GET** /api/admin/v1/skills/{name} | Get skill details|
|[**apiAdminV1TasksGet**](#apiadminv1tasksget) | **GET** /api/admin/v1/tasks | List all recurring tasks|
|[**apiAdminV1TasksIdGet**](#apiadminv1tasksidget) | **GET** /api/admin/v1/tasks/{id} | Get task details|
|[**apiV1FilesFilepathGet**](#apiv1filesfilepathget) | **GET** /api/v1/files/{filepath} | Download a file from the local storage|
|[**apiV1InteractionPost**](#apiv1interactionpost) | **POST** /api/v1/interaction | Manage sessions or check global status|
|[**apiV1PromptPost**](#apiv1promptpost) | **POST** /api/v1/prompt | Send a prompt to the agent|
|[**apiV1PromptStreamGet**](#apiv1promptstreamget) | **GET** /api/v1/prompt/stream | Stream a prompt response via SSE|
|[**wsGet**](#wsget) | **GET** /ws | WebSocket for interactive streaming|

# **apiAdminV1ChannelsPost**
> apiAdminV1ChannelsPost(apiAdminV1ChannelsPostRequest)


### Example

```typescript
import {
    DefaultApi,
    Configuration,
    ApiAdminV1ChannelsPostRequest
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let apiAdminV1ChannelsPostRequest: ApiAdminV1ChannelsPostRequest; //

const { status, data } = await apiInstance.apiAdminV1ChannelsPost(
    apiAdminV1ChannelsPostRequest
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **apiAdminV1ChannelsPostRequest** | **ApiAdminV1ChannelsPostRequest**|  | |


### Return type

void (empty response body)

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: Not defined


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Action performed |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1ConfigGet**
> Config apiAdminV1ConfigGet()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

const { status, data } = await apiInstance.apiAdminV1ConfigGet();
```

### Parameters
This endpoint does not have any parameters.


### Return type

**Config**

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Current config |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1ConfigPost**
> apiAdminV1ConfigPost(config)


### Example

```typescript
import {
    DefaultApi,
    Configuration,
    Config
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let config: Config; //

const { status, data } = await apiInstance.apiAdminV1ConfigPost(
    config
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **config** | **Config**|  | |


### Return type

void (empty response body)

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: Not defined


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Config updated |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1HealthGet**
> ApiAdminV1HealthGet200Response apiAdminV1HealthGet()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

const { status, data } = await apiInstance.apiAdminV1HealthGet();
```

### Parameters
This endpoint does not have any parameters.


### Return type

**ApiAdminV1HealthGet200Response**

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Healthy |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1HumanGet**
> Array<HumanInfo> apiAdminV1HumanGet()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

const { status, data } = await apiInstance.apiAdminV1HumanGet();
```

### Parameters
This endpoint does not have any parameters.


### Return type

**Array<HumanInfo>**

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | List of human info |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1HumanPost**
> apiAdminV1HumanPost(humanInfo)


### Example

```typescript
import {
    DefaultApi,
    Configuration,
    HumanInfo
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let humanInfo: HumanInfo; //

const { status, data } = await apiInstance.apiAdminV1HumanPost(
    humanInfo
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **humanInfo** | **HumanInfo**|  | |


### Return type

void (empty response body)

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: Not defined


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Human info saved |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1SessionsGet**
> Array<string> apiAdminV1SessionsGet()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

const { status, data } = await apiInstance.apiAdminV1SessionsGet();
```

### Parameters
This endpoint does not have any parameters.


### Return type

**Array<string>**

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | List of session IDs |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1SessionsIdGet**
> Session apiAdminV1SessionsIdGet()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let id: string; // (default to undefined)

const { status, data } = await apiInstance.apiAdminV1SessionsIdGet(
    id
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **id** | [**string**] |  | defaults to undefined|


### Return type

**Session**

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Session details |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1SessionsIdHistoryGet**
> ApiAdminV1SessionsIdHistoryGet200Response apiAdminV1SessionsIdHistoryGet()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let id: string; // (default to undefined)

const { status, data } = await apiInstance.apiAdminV1SessionsIdHistoryGet(
    id
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **id** | [**string**] |  | defaults to undefined|


### Return type

**ApiAdminV1SessionsIdHistoryGet200Response**

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Session history |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1SessionsIdSkillsGet**
> Array<string> apiAdminV1SessionsIdSkillsGet()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let id: string; // (default to undefined)

const { status, data } = await apiInstance.apiAdminV1SessionsIdSkillsGet(
    id
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **id** | [**string**] |  | defaults to undefined|


### Return type

**Array<string>**

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | List of loaded skill names |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1SessionsIdStatsGet**
> ApiAdminV1SessionsIdStatsGet200Response apiAdminV1SessionsIdStatsGet()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let id: string; // (default to undefined)

const { status, data } = await apiInstance.apiAdminV1SessionsIdStatsGet(
    id
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **id** | [**string**] |  | defaults to undefined|


### Return type

**ApiAdminV1SessionsIdStatsGet200Response**

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Session statistics |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1SkillsCommandsGet**
> Array<SkillCommand> apiAdminV1SkillsCommandsGet()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

const { status, data } = await apiInstance.apiAdminV1SkillsCommandsGet();
```

### Parameters
This endpoint does not have any parameters.


### Return type

**Array<SkillCommand>**

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | List of available commands |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1SkillsGet**
> Array<Skill> apiAdminV1SkillsGet()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

const { status, data } = await apiInstance.apiAdminV1SkillsGet();
```

### Parameters
This endpoint does not have any parameters.


### Return type

**Array<Skill>**

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | List of skills |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1SkillsNameDelete**
> apiAdminV1SkillsNameDelete()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let name: string; // (default to undefined)

const { status, data } = await apiInstance.apiAdminV1SkillsNameDelete(
    name
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **name** | [**string**] |  | defaults to undefined|


### Return type

void (empty response body)

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Skill removed |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1SkillsNameGet**
> Skill apiAdminV1SkillsNameGet()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let name: string; // (default to undefined)

const { status, data } = await apiInstance.apiAdminV1SkillsNameGet(
    name
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **name** | [**string**] |  | defaults to undefined|


### Return type

**Skill**

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Skill details |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1TasksGet**
> Array<Task> apiAdminV1TasksGet()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

const { status, data } = await apiInstance.apiAdminV1TasksGet();
```

### Parameters
This endpoint does not have any parameters.


### Return type

**Array<Task>**

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | List of tasks |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiAdminV1TasksIdGet**
> Task apiAdminV1TasksIdGet()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let id: string; // (default to undefined)

const { status, data } = await apiInstance.apiAdminV1TasksIdGet(
    id
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **id** | [**string**] |  | defaults to undefined|


### Return type

**Task**

### Authorization

[BasicAuth](../README.md#BasicAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Task details |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1FilesFilepathGet**
> File apiV1FilesFilepathGet()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let filepath: string; // (default to undefined)

const { status, data } = await apiInstance.apiV1FilesFilepathGet(
    filepath
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **filepath** | [**string**] |  | defaults to undefined|


### Return type

**File**

### Authorization

[ServerKey](../README.md#ServerKey)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/octet-stream


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | The file content |  -  |
|**404** | File not found |  -  |
|**403** | Access denied |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1InteractionPost**
> apiV1InteractionPost(apiV1InteractionPostRequest)


### Example

```typescript
import {
    DefaultApi,
    Configuration,
    ApiV1InteractionPostRequest
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let apiV1InteractionPostRequest: ApiV1InteractionPostRequest; //

const { status, data } = await apiInstance.apiV1InteractionPost(
    apiV1InteractionPostRequest
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **apiV1InteractionPostRequest** | **ApiV1InteractionPostRequest**|  | |


### Return type

void (empty response body)

### Authorization

[ServerKey](../README.md#ServerKey)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: Not defined


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Successful response |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1PromptPost**
> ApiV1PromptPost200Response apiV1PromptPost(apiV1PromptPostRequest)


### Example

```typescript
import {
    DefaultApi,
    Configuration,
    ApiV1PromptPostRequest
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let apiV1PromptPostRequest: ApiV1PromptPostRequest; //

const { status, data } = await apiInstance.apiV1PromptPost(
    apiV1PromptPostRequest
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **apiV1PromptPostRequest** | **ApiV1PromptPostRequest**|  | |


### Return type

**ApiV1PromptPost200Response**

### Authorization

[ServerKey](../README.md#ServerKey)

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | Successful response |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **apiV1PromptStreamGet**
> string apiV1PromptStreamGet()


### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let prompt: string; // (default to undefined)
let model: string; // (optional) (default to undefined)

const { status, data } = await apiInstance.apiV1PromptStreamGet(
    prompt,
    model
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **prompt** | [**string**] |  | defaults to undefined|
| **model** | [**string**] |  | (optional) defaults to undefined|


### Return type

**string**

### Authorization

[ServerKey](../README.md#ServerKey)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: text/event-stream


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**200** | SSE stream of response chunks |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **wsGet**
> wsGet()

WebSocket endpoint for real-time interaction and background task notifications. When a background task completes, a message is sent in the following format: ```json {   \"type\": \"task_complete\",   \"task_id\": \"uuid\",   \"task_name\": \"task name\",   \"response\": \"result string\" } ``` 

### Example

```typescript
import {
    DefaultApi,
    Configuration
} from './api';

const configuration = new Configuration();
const apiInstance = new DefaultApi(configuration);

let channel: string; // (optional) (default to undefined)
let device: string; // (optional) (default to undefined)

const { status, data } = await apiInstance.wsGet(
    channel,
    device
);
```

### Parameters

|Name | Type | Description  | Notes|
|------------- | ------------- | ------------- | -------------|
| **channel** | [**string**] |  | (optional) defaults to undefined|
| **device** | [**string**] |  | (optional) defaults to undefined|


### Return type

void (empty response body)

### Authorization

[ServerKey](../README.md#ServerKey)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined


### HTTP response details
| Status code | Description | Response headers |
|-------------|-------------|------------------|
|**101** | WebSocket upgrade |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)


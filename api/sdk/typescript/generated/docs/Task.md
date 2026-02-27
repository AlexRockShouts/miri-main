# Task


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **string** |  | [optional] [default to undefined]
**name** | **string** |  | [optional] [default to undefined]
**cron_expression** | **string** |  | [optional] [default to undefined]
**prompt** | **string** |  | [optional] [default to undefined]
**active** | **boolean** |  | [optional] [default to undefined]
**needed_skills** | **Array&lt;string&gt;** |  | [optional] [default to undefined]
**last_run** | **string** |  | [optional] [default to undefined]
**created** | **string** |  | [optional] [default to undefined]
**updated** | **string** |  | [optional] [default to undefined]
**report_session** | **string** |  | [optional] [default to undefined]
**report_channels** | **Array&lt;string&gt;** |  | [optional] [default to undefined]

## Example

```typescript
import { Task } from './api';

const instance: Task = {
    id,
    name,
    cron_expression,
    prompt,
    active,
    needed_skills,
    last_run,
    created,
    updated,
    report_session,
    report_channels,
};
```

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)

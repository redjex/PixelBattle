# WebSocket protocol

Client placement:

```json
{"type":"place_pixel","boardId":"main","x":12,"y":8,"color":"#3D87E8CC","operationId":"uuid"}
```

Server acceptance:

```json
{"type":"pixel_placed","eventId":"uuid","boardId":"main","x":12,"y":8,"color":"#3D87E8CC","version":42}
```

Errors use `{ "type": "error", "code": "...", "message": "..." }`.

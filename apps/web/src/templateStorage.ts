export type TemplatePlacement = {
  x: number;
  y: number;
  width: number;
  height: number;
};

type StoredTemplate = {
  userId: string;
  image: Blob;
  placement?: TemplatePlacement;
  updatedAt: number;
};

const DATABASE_NAME = 'pixelbattle-user-state';
const DATABASE_VERSION = 1;
const STORE_NAME = 'map-templates';
const pendingWrites = new Map<string, Promise<void>>();

function enqueueWrite(userId: string, action: () => Promise<void>) {
  const previous = pendingWrites.get(userId) ?? Promise.resolve();
  const next = previous.catch(() => undefined).then(action);
  pendingWrites.set(userId, next);
  void next.finally(() => {
    if (pendingWrites.get(userId) === next) pendingWrites.delete(userId);
  }).catch(() => undefined);
  return next;
}

function openDatabase() {
  return new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open(DATABASE_NAME, DATABASE_VERSION);
    request.onupgradeneeded = () => {
      const database = request.result;
      if (!database.objectStoreNames.contains(STORE_NAME)) database.createObjectStore(STORE_NAME, { keyPath: 'userId' });
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error('Unable to open template storage'));
  });
}

function requestResult<T>(request: IDBRequest<T>) {
  return new Promise<T>((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error('Template storage request failed'));
  });
}

async function readTemplate(database: IDBDatabase, userId: string) {
  const transaction = database.transaction(STORE_NAME, 'readonly');
  return requestResult(transaction.objectStore(STORE_NAME).get(userId)) as Promise<StoredTemplate | undefined>;
}

export async function loadTemplate(userId: string) {
  const database = await openDatabase();
  try {
    return await readTemplate(database, userId);
  } finally {
    database.close();
  }
}

export async function saveTemplateImage(userId: string, image: Blob) {
  return enqueueWrite(userId, async () => {
    const database = await openDatabase();
    try {
      const transaction = database.transaction(STORE_NAME, 'readwrite');
      await requestResult(transaction.objectStore(STORE_NAME).put({ userId, image, updatedAt: Date.now() } satisfies StoredTemplate));
    } finally {
      database.close();
    }
  });
}

export async function saveTemplatePlacement(userId: string, placement: TemplatePlacement) {
  return enqueueWrite(userId, async () => {
    const database = await openDatabase();
    try {
      const current = await readTemplate(database, userId);
      if (!current) return;
      const transaction = database.transaction(STORE_NAME, 'readwrite');
      await requestResult(transaction.objectStore(STORE_NAME).put({ ...current, placement, updatedAt: Date.now() } satisfies StoredTemplate));
    } finally {
      database.close();
    }
  });
}

export async function removeTemplate(userId: string) {
  return enqueueWrite(userId, async () => {
    const database = await openDatabase();
    try {
      const transaction = database.transaction(STORE_NAME, 'readwrite');
      await requestResult(transaction.objectStore(STORE_NAME).delete(userId));
    } finally {
      database.close();
    }
  });
}

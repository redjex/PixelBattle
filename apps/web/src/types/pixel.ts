export type PixelAuthor = {
  id: string;
  displayName?: string;
  username?: string;
  photoUrl?: string;
};

export type Pixel = {
  x: number;
  y: number;
  color: string;
  version?: number;
  operationId?: string;
  author?: PixelAuthor;
  frozenUntil?: string;
};

export type PlacementMessage = Pixel & {
  type: 'place_pixel';
  boardId: string;
  operationId: string;
  useIce?: boolean;
};

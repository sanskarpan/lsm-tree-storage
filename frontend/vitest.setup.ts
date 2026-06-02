import "@testing-library/jest-dom/vitest";
import { expect, afterEach } from "vitest";
import { cleanup } from "@testing-library/react";
import * as matchers from "jest-extended";

expect.extend(matchers);
afterEach(() => cleanup());

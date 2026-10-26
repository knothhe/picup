#!/usr/bin/env python3
"""Derive badged Dev icons without modifying the original application artwork."""

from __future__ import annotations

import argparse
import hashlib
import io
import json
from pathlib import Path
import re
import shutil
import stat
import sys
import tempfile


TOOL = "h2-desktop-client/dev-icon"
MANIFEST = ".dev-icon-manifest.json"
PNG_SIZES = (16, 24, 32, 48, 64, 128, 256, 512, 1024)
ICO_SIZES = (16, 20, 24, 32, 40, 48, 64, 128, 256)
# Pillow writes standard ICNS slots, including 16@2x and 32@2x representations.
ICNS_SIZES = (32, 64, 128, 256, 512, 1024)
MAX_EDGE = 4096


class IconError(Exception):
    pass


def load_pillow():
    global Image, ImageDraw, ImageFont, PngInfo
    try:
        from PIL import Image, ImageDraw, ImageFont
        from PIL.PngImagePlugin import PngInfo
    except ImportError as exc:
        raise IconError(
            "Pillow >= 10.1 is required. Install in your chosen Python environment: "
            "python3 -m pip install 'Pillow>=10.1'"
        ) from exc
    try:
        ImageFont.load_default(size=12)
    except (TypeError, ImportError) as exc:
        raise IconError("Pillow >= 10.1 with its bundled scalable font is required.") from exc


def parser():
    p = argparse.ArgumentParser(
        description="Add a bottom-right Dev badge and generate PNG/ICO/ICNS in a separate directory.",
        epilog="Requires Python >= 3.9 and Pillow >= 10.1. Input artwork is never modified.",
    )
    p.add_argument("--input", required=True, type=Path, help="original square PNG, ICO or ICNS")
    p.add_argument("--output-dir", required=True, type=Path, help="dedicated Dev resource directory")
    p.add_argument("--formats", default="png,ico,icns", help="comma-separated png,ico,icns")
    p.add_argument("--sizes", help="PNG pixel sizes, e.g. 32,64,128,256; default: standard sizes up to source size, plus source size")
    p.add_argument("--name", default="app-icon-dev", help="output basename without extension (default: app-icon-dev)")
    p.add_argument("--allow-upscale", action="store_true", help="explicitly allow outputs larger than the source artwork")
    p.add_argument("--force", action="store_true", help="regenerate verified outputs owned by this script; preserve unrelated files")
    return p


def digest(data):
    return hashlib.sha256(data).hexdigest()


def checked_edge(width, height):
    if width != height or not 16 <= width <= MAX_EDGE:
        raise IconError(f"Artwork must be square and between 16 and {MAX_EDGE} pixels; got {width}x{height}.")


def read_source(source):
    data = source.read_bytes()
    with Image.open(io.BytesIO(data)) as image:
        if image.info.get(TOOL):
            raise IconError("Input is already a generated Dev PNG; use the original artwork to avoid double badging.")
        if image.format not in {"PNG", "ICO", "ICNS"}:
            raise IconError("Supported inputs are PNG, ICO and ICNS; export SVG or native catalogs with the project's existing tools first.")
        checked_edge(*image.size)
        frames = {}
        if image.format == "ICO":
            for width, height in sorted(image.ico.sizes()):
                checked_edge(width, height)
                frames[width] = image.ico.getimage((width, height)).convert("RGBA")
        elif image.format == "ICNS":
            for width, height, scale in sorted(image.icns.itersizes(), key=lambda size: (size[0] * size[2], size[2])):
                edge = width * scale
                checked_edge(edge, height * scale)
                if edge not in frames:
                    frames[edge] = image.icns.getimage((width, height, scale)).convert("RGBA")
        else:
            if getattr(image, "n_frames", 1) != 1:
                raise IconError("Animated PNG inputs are not supported.")
            frames[image.width] = image.convert("RGBA")
    if not frames:
        raise IconError("No usable icon representations found.")
    # Container formats cannot carry the standalone PNG marker reliably.
    manifest = source.parent / MANIFEST
    if manifest.is_file() and not manifest.is_symlink():
        try:
            record = json.loads(manifest.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            record = {}
        if isinstance(record, dict) and record.get("tool") == TOOL and isinstance(record.get("files"), dict) and source.name in record["files"]:
            raise IconError("Input belongs to generated Dev resources; use the original artwork.")
    return frames, digest(data)


def source_frame(frames, edge):
    # Retain small optical corrections when the source container provides them.
    choice = next((size for size in sorted(frames) if size >= edge), max(frames))
    image = frames[choice]
    return image.copy() if choice == edge else image.resize((edge, edge), Image.Resampling.LANCZOS)


def badge(image):
    """Render each size independently; preserve pixels outside the badge rectangle."""
    edge = image.width
    # Keep tiny lettering pixel-aligned; shrinking extra strokes blurs the counters.
    scale = 1 if edge <= 32 else 4
    minimum_font = 6 if edge < 20 else 7
    font = ImageFont.load_default(size=max(minimum_font, round(edge * 0.21)) * scale)
    stroke = 0 if scale == 1 else max(1, round(edge * scale * 0.002))
    bounds = font.getbbox("Dev", stroke_width=stroke)
    pad_x, pad_y = max(1, round(edge * 0.045)) * scale, max(1, round(edge * 0.03)) * scale
    width = bounds[2] - bounds[0] + pad_x * 2
    height = bounds[3] - bounds[1] + pad_y * 2
    layer = Image.new("RGBA", (width, height))
    drawing = ImageDraw.Draw(layer)
    drawing.rounded_rectangle(
        (0, 0, width - 1, height - 1),
        radius=round(height * 0.25),
        fill=(23, 25, 31, 255),
        outline=(255, 255, 255, 245),
        width=max(scale, round(edge * 0.006) * scale),
    )
    drawing.text((pad_x - bounds[0], pad_y - bounds[1]), "Dev", font=font, fill="white",
                 stroke_width=stroke, stroke_fill="white")
    layer = layer.resize((round(width / scale), round(height / scale)), Image.Resampling.LANCZOS)
    margin = max(1, round(edge * 0.035))
    left, top = edge - margin - layer.width, edge - margin - layer.height
    if left < 0 or top < 0:
        raise IconError(f"Dev badge does not fit at {edge}px.")
    result = image.copy()
    result.alpha_composite(layer, (left, top))
    return result


def parse_options(args, largest):
    formats = [word.strip().lower() for word in args.formats.split(",")]
    if not formats or len(set(formats)) != len(formats) or any(word not in {"png", "ico", "icns"} for word in formats):
        raise IconError("--formats must contain distinct values from png,ico,icns.")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,79}", args.name):
        raise IconError("--name must be a filename basename using letters, numbers, dot, dash or underscore.")
    if args.sizes and "png" not in formats:
        raise IconError("--sizes only applies to PNG; include png in --formats.")
    if args.sizes is None:
        sizes = sorted({size for size in PNG_SIZES if size <= largest} | {largest})
    else:
        try:
            sizes = sorted({int(word.strip()) for word in args.sizes.split(",")})
        except ValueError as exc:
            raise IconError("--sizes must be comma-separated integers.") from exc
        for size in sizes:
            checked_edge(size, size)
    required = set(sizes if "png" in formats else [])
    if "ico" in formats:
        required.update(ICO_SIZES)
    if "icns" in formats:
        required.update(ICNS_SIZES)
    if max(required) > largest and not args.allow_upscale:
        raise IconError(
            f"Source has {largest}px artwork, but selected outputs require {max(required)}px. "
            "Use higher-resolution PNG/ICO/ICNS artwork, omit that format, or explicitly pass --allow-upscale."
        )
    return formats, sizes, required


def owned_outputs(output, force):
    if output.is_symlink() or (output.exists() and not output.is_dir()):
        raise IconError("Output must be a real directory, not a file or symbolic link.")
    if not output.exists() or not any(output.iterdir()):
        return {}
    manifest = output / MANIFEST
    if not manifest.is_file() or manifest.is_symlink():
        raise IconError("Nonempty output directory is not managed by this script; choose a separate Dev directory.")
    try:
        record = json.loads(manifest.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise IconError("Output ownership manifest is unreadable; existing resources were kept.") from exc
    if not isinstance(record, dict) or record.get("tool") != TOOL or record.get("schema") != 1 or not isinstance(record.get("files"), dict):
        raise IconError("Output ownership manifest is not recognized.")
    files = record["files"]
    if not files:
        raise IconError("Output ownership manifest has no generated files.")
    for name, expected in files.items():
        if not isinstance(name, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,100}\.(png|ico|icns)", name):
            raise IconError("Invalid generated filename in ownership manifest.")
        path = output / name
        if not isinstance(expected, str) or not re.fullmatch(r"[0-9a-f]{64}", expected):
            raise IconError("Invalid generated checksum in ownership manifest.")
        if path.is_symlink() or (path.exists() and (not path.is_file() or digest(path.read_bytes()) != expected)):
            raise IconError(f"Generated resource was changed or replaced: {name}; existing resources were kept.")
    if not force:
        raise IconError("Dev resources already exist. Pass --force to regenerate verified script-owned outputs.")
    return files


def directory_snapshot(directory):
    """Detect changes to unrelated files before replacing a staged directory."""
    snapshot = {}
    for path in [directory, *sorted(directory.rglob("*"))]:
        status = path.lstat()
        key = path.relative_to(directory).as_posix()
        if stat.S_ISLNK(status.st_mode):
            content = str(path.readlink())
        elif stat.S_ISREG(status.st_mode):
            content = digest(path.read_bytes())
        elif stat.S_ISDIR(status.st_mode):
            content = None
        else:
            raise IconError(f"Output directory contains an unsupported file: {key}.")
        snapshot[key] = (status.st_mode, status.st_mtime_ns, content)
    return snapshot


def write_resources(stage, frames, args, formats, sizes, required):
    rendered = {size: badge(source_frame(frames, size)) for size in sorted(required)}
    names = []

    def reserve(name):
        path = stage / name
        if path.exists() or path.is_symlink():
            raise IconError(f"New output would overwrite an unrelated file: {name}.")
        names.append(name)
        return path

    if "png" in formats:
        info = PngInfo()
        info.add_text(TOOL, "1")
        for size in sizes:
            rendered[size].save(reserve(f"{args.name}-{size}.png"), format="PNG", pnginfo=info)
        rendered[max(sizes)].save(reserve(f"{args.name}.png"), format="PNG", pnginfo=info)
    if "ico" in formats:
        rendered[256].save(
            reserve(f"{args.name}.ico"), format="ICO",
            sizes=[(size, size) for size in ICO_SIZES],
            append_images=[rendered[size] for size in ICO_SIZES if size != 256],
        )
    if "icns" in formats:
        rendered[1024].save(
            reserve(f"{args.name}.icns"), format="ICNS",
            append_images=[rendered[size] for size in ICNS_SIZES],
        )
    return {name: digest((stage / name).read_bytes()) for name in sorted(names)}


def generate(args):
    if args.output_dir.is_symlink():
        raise IconError("Output directory cannot be a symbolic link.")
    source = args.input.resolve(strict=True)
    output = args.output_dir.resolve()
    if not source.is_file():
        raise IconError("Input must be a regular image file.")
    if source.is_relative_to(output):
        raise IconError("Output directory must not contain the original input artwork.")
    frames, source_hash = read_source(source)
    formats, sizes, required = parse_options(args, max(frames))
    old_files = owned_outputs(output, args.force)
    original_snapshot = directory_snapshot(output) if output.exists() else None
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".dev-icon-", dir=output.parent) as temporary:
        stage = Path(temporary) / "resources"
        if output.exists():
            shutil.copytree(output, stage, symlinks=True)
            for name in old_files:
                (stage / name).unlink(missing_ok=True)
            (stage / MANIFEST).unlink(missing_ok=True)
        else:
            stage.mkdir()
        files = write_resources(stage, frames, args, formats, sizes, required)
        record = {"schema": 1, "tool": TOOL, "source": source.name, "source_sha256": source_hash,
                  "generator_sha256": digest(Path(__file__).read_bytes()),
                  "name": args.name, "formats": sorted(formats),
                  "png_sizes": sizes if "png" in formats else [], "files": files}
        (stage / MANIFEST).write_text(json.dumps(record, ensure_ascii=False, sort_keys=True, indent=2) + "\n", encoding="utf-8")
        # Recheck ownership after rendering; don't overwrite a concurrent manual edit.
        if output.exists():
            if owned_outputs(output, args.force) != old_files or directory_snapshot(output) != original_snapshot:
                raise IconError("Output changed during generation; existing resources were kept.")
            # Keep a failed recovery outside TemporaryDirectory's automatic cleanup.
            backup = Path(tempfile.mkdtemp(prefix=f".{output.name}-backup-", dir=output.parent))
            backup.rmdir()
            output.rename(backup)
            try:
                if directory_snapshot(backup) != original_snapshot:
                    raise IconError("Output changed during replacement; existing resources were kept.")
                stage.rename(output)
            except (OSError, IconError) as exc:
                try:
                    backup.rename(output)
                except OSError as recovery:
                    raise IconError(f"Replacement and recovery failed; previous resources are preserved at {backup}: {recovery}") from exc
                raise
            try:
                shutil.rmtree(backup)
            except OSError as exc:
                print(f"dev-icon: generated resources are ready; previous directory kept at {backup}: {exc}", file=sys.stderr)
        else:
            if original_snapshot is not None:
                raise IconError("Output was removed during generation; replacement was stopped.")
            stage.rename(output)
    return output, files


def main(argv=None):
    args = parser().parse_args(argv)
    try:
        load_pillow()
    except IconError as exc:
        print(f"dev-icon: {exc}", file=sys.stderr)
        return 1
    try:
        output, files = generate(args)
    except (IconError, OSError, ValueError, Image.DecompressionBombError) as exc:
        print(f"dev-icon: {exc}", file=sys.stderr)
        return 1
    print(f"Generated {len(files)} Dev icon files in {output}")
    for name in files:
        print(f"  {name}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

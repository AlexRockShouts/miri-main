#!/usr/bin/env python3
"""
Quick validation script for skills - minimal version
"""

import sys
import os
import re
from pathlib import Path

def validate_skill(skill_path):
    """Basic validation of a skill (folder or flat .md file)"""
    skill_path = Path(skill_path)
    
    # Check if path exists
    if not skill_path.exists():
        return False, f"Skill path not found: {skill_path}"

    # Determine SKILL.md or flat .md file
    if skill_path.is_file() and skill_path.suffix == '.md':
        skill_md = skill_path
    elif skill_path.is_dir():
        # Check for SKILL.md first (legacy) then name.md
        skill_md = skill_path / 'SKILL.md'
        if not skill_md.exists():
            # Try to find a .md file with the same name as the directory
            skill_md = skill_path.parent / f"{skill_path.name}.md"
            if not skill_md.exists():
                return False, f"Neither SKILL.md (in {skill_path}) nor {skill_path.name}.md found"
    else:
        return False, f"Invalid skill path: {skill_path}"
    
    # Read and validate frontmatter
    content = skill_md.read_text()
    if not content.startswith('---'):
        return False, "No YAML frontmatter found"
    
    # Extract frontmatter
    match = re.match(r'^---\n(.*?)\n---', content, re.DOTALL)
    if not match:
        return False, "Invalid frontmatter format"
    
    frontmatter = match.group(1)
    
    # Check required fields
    if 'name:' not in frontmatter:
        return False, "Missing 'name' in frontmatter"
    if 'description:' not in frontmatter:
        return False, "Missing 'description' in frontmatter"
    
    # Extract name for validation
    name_match = re.search(r'name:\s*(.+)', frontmatter)
    if name_match:
        name = name_match.group(1).strip()
        # Check naming convention (hyphen-case: lowercase with hyphens)
        if not re.match(r'^[a-z0-9-]+$', name):
            return False, f"Name '{name}' should be hyphen-case (lowercase letters, digits, and hyphens only)"
        if name.startswith('-') or name.endswith('-') or '--' in name:
            return False, f"Name '{name}' cannot start/end with hyphen or contain consecutive hyphens"

    # Extract and validate description
    desc_match = re.search(r'description:\s*(.+)', frontmatter)
    if desc_match:
        description = desc_match.group(1).strip()
        # Check for angle brackets
        if '<' in description or '>' in description:
            return False, "Description cannot contain angle brackets (< or >)"

    return True, "Skill is valid!"

if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("Usage: python quick_validate.py <skill_directory>")
        sys.exit(1)
    
    valid, message = validate_skill(sys.argv[1])
    print(message)
    sys.exit(0 if valid else 1)